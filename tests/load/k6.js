import http from "k6/http";
import { check, sleep, group } from "k6";
import { Rate, Trend } from "k6/metrics";

// Custom metrics
const orderCreationDuration = new Trend("order_creation_duration", true);
const orderFailRate = new Rate("order_fail_rate");

// Test configuration
const BASE_URL = __ENV.BASE_URL || "http://localhost:8080";

export const options = {
  scenarios: {
    // Ramp up to steady state
    steady_load: {
      executor: "ramping-vus",
      startVUs: 1,
      stages: [
        { duration: "30s", target: 10 }, // ramp up
        { duration: "2m", target: 10 }, // steady state
        { duration: "30s", target: 20 }, // spike
        { duration: "1m", target: 20 }, // sustained spike
        { duration: "30s", target: 0 }, // ramp down
      ],
    },
  },
  thresholds: {
    http_req_duration: ["p(95)<500", "p(99)<1000"],
    http_req_failed: ["rate<0.01"],
    order_creation_duration: ["p(95)<800"],
    order_fail_rate: ["rate<0.05"],
  },
};

// Setup: create test products
export function setup() {
  const products = [];

  for (let i = 0; i < 5; i++) {
    const res = http.post(
      `${BASE_URL}/api/v1/products`,
      JSON.stringify({
        name: `Load Test Product ${i}`,
        category: "load-test",
        price_cents: 1000 + i * 500,
        initial_stock: 10000,
      }),
      { headers: { "Content-Type": "application/json" } }
    );

    if (res.status === 201) {
      const product = JSON.parse(res.body);
      products.push(product.id);
    }
  }

  console.log(`Created ${products.length} test products`);
  return { products };
}

export default function (data) {
  const products = data.products;

  if (products.length === 0) {
    console.error("No products available");
    return;
  }

  group("Create Order Flow", () => {
    // Pick random product
    const productId = products[Math.floor(Math.random() * products.length)];
    const quantity = Math.floor(Math.random() * 3) + 1;

    const start = Date.now();
    const res = http.post(
      `${BASE_URL}/api/v1/orders`,
      JSON.stringify({
        customer_id: `load-test-${__VU}`,
        items: [{ product_id: productId, quantity: quantity }],
      }),
      { headers: { "Content-Type": "application/json" } }
    );
    const duration = Date.now() - start;

    orderCreationDuration.add(duration);

    const success = check(res, {
      "order created": (r) => r.status === 201,
      "has order id": (r) => {
        try {
          return JSON.parse(r.body).id !== "";
        } catch {
          return false;
        }
      },
      "order completed": (r) => {
        try {
          return JSON.parse(r.body).status === "ORDER_STATUS_COMPLETED";
        } catch {
          return false;
        }
      },
    });

    orderFailRate.add(!success);

    if (res.status === 201) {
      const order = JSON.parse(res.body);

      // Verify we can fetch the order
      const getRes = http.get(`${BASE_URL}/api/v1/orders/${order.id}`);
      check(getRes, {
        "get order ok": (r) => r.status === 200,
      });
    }
  });

  group("List Products", () => {
    const res = http.get(`${BASE_URL}/api/v1/products?page_size=10`);
    check(res, {
      "list products ok": (r) => r.status === 200,
    });
  });

  group("Search Products", () => {
    const res = http.get(`${BASE_URL}/api/v1/products/search?q=Load+Test`);
    check(res, {
      "search ok": (r) => r.status === 200,
    });
  });

  sleep(Math.random() * 0.5);
}

export function teardown(data) {
  console.log("Load test complete");
}
