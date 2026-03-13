package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// OrderEvent represents a completed order event for analytics.
type OrderEvent struct {
	OrderID    string
	CustomerID string
	Status     string
	TotalCents int64
	ItemCount  int32
	CreatedAt  time.Time
	CompletedAt time.Time
}

// ClickHouseClient manages the ClickHouse connection and batch inserts.
type ClickHouseClient struct {
	conn    driver.Conn
	log     *slog.Logger
	events  chan OrderEvent
	done    chan struct{}
	wg      sync.WaitGroup
}

// NewClickHouseClient creates a ClickHouse connection and starts the consumer.
func NewClickHouseClient(addr, database, username, password string, log *slog.Logger) (*ClickHouseClient, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: username,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout: 10 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	c := &ClickHouseClient{
		conn:   conn,
		log:    log,
		events: make(chan OrderEvent, 1000),
		done:   make(chan struct{}),
	}

	return c, nil
}

// CreateTables creates the analytics tables.
func (c *ClickHouseClient) CreateTables(ctx context.Context) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS order_events (
			order_id String,
			customer_id String,
			status String,
			total_cents Int64,
			item_count Int32,
			created_at DateTime64(3),
			completed_at DateTime64(3),
			inserted_at DateTime64(3) DEFAULT now64(3)
		) ENGINE = MergeTree()
		ORDER BY (completed_at, customer_id)`,

		`CREATE TABLE IF NOT EXISTS revenue_daily (
			date Date,
			total_revenue Int64,
			order_count Int64,
			avg_order_value Float64
		) ENGINE = SummingMergeTree()
		ORDER BY date`,

		`CREATE MATERIALIZED VIEW IF NOT EXISTS revenue_daily_mv
		TO revenue_daily AS
		SELECT
			toDate(completed_at) AS date,
			sum(total_cents) AS total_revenue,
			count() AS order_count,
			avg(total_cents) AS avg_order_value
		FROM order_events
		WHERE status = 'COMPLETED'
		GROUP BY date`,
	}

	for _, q := range queries {
		if err := c.conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	c.log.Info("clickhouse tables created")
	return nil
}

// Publish sends an order event to the consumer channel.
func (c *ClickHouseClient) Publish(event OrderEvent) {
	select {
	case c.events <- event:
	default:
		c.log.Warn("clickhouse event channel full, dropping event",
			slog.String("order_id", event.OrderID))
	}
}

// StartConsumer starts the background batch insert goroutine.
// Flushes every flushInterval or when batchSize events are buffered.
func (c *ClickHouseClient) StartConsumer(batchSize int, flushInterval time.Duration) {
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()

		batch := make([]OrderEvent, 0, batchSize)
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()

		for {
			select {
			case event := <-c.events:
				batch = append(batch, event)
				if len(batch) >= batchSize {
					c.flush(batch)
					batch = batch[:0]
				}

			case <-ticker.C:
				if len(batch) > 0 {
					c.flush(batch)
					batch = batch[:0]
				}

			case <-c.done:
				// Drain remaining events
				for {
					select {
					case event := <-c.events:
						batch = append(batch, event)
					default:
						if len(batch) > 0 {
							c.flush(batch)
						}
						return
					}
				}
			}
		}
	}()
}

func (c *ClickHouseClient) flush(events []OrderEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	batch, err := c.conn.PrepareBatch(ctx, `
		INSERT INTO order_events (order_id, customer_id, status, total_cents, item_count, created_at, completed_at)`)
	if err != nil {
		c.log.Error("prepare clickhouse batch", slog.String("error", err.Error()))
		return
	}

	for _, e := range events {
		err := batch.Append(e.OrderID, e.CustomerID, e.Status, e.TotalCents, e.ItemCount, e.CreatedAt, e.CompletedAt)
		if err != nil {
			c.log.Error("append to batch", slog.String("error", err.Error()), slog.String("order_id", e.OrderID))
		}
	}

	if err := batch.Send(); err != nil {
		c.log.Error("send clickhouse batch", slog.String("error", err.Error()))
	} else {
		c.log.Info("flushed events to clickhouse", slog.Int("count", len(events)))
	}
}

// Close stops the consumer and closes the connection.
func (c *ClickHouseClient) Close() {
	close(c.done)
	c.wg.Wait()
	c.conn.Close()
}

type DailyRevenue struct {
	Date          time.Time `ch:"date"`
	TotalRevenue  int64     `ch:"total_revenue"`
	OrderCount    int64     `ch:"order_count"`
	AvgOrderValue float64   `ch:"avg_order_value"`
}

type TopProduct struct {
	ProductID  string `ch:"product_id"`
	OrderCount int64  `ch:"order_count"`
	Revenue    int64  `ch:"revenue"`
}

func (c *ClickHouseClient) GetDailyRevenue(ctx context.Context, days int) ([]DailyRevenue, error) {
	var results []DailyRevenue
	err := c.conn.Select(ctx, &results, `
		SELECT date, total_revenue, order_count, avg_order_value
		FROM revenue_daily
		WHERE date >= today() - $1
		ORDER BY date DESC`, days)
	if err != nil {
		return nil, fmt.Errorf("query daily revenue: %w", err)
	}
	return results, nil
}
