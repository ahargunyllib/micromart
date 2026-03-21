FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.work go.work.sum ./
COPY pkg/ pkg/
COPY gen/ gen/
COPY services/order/ services/order/

WORKDIR /app/services/order
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /order-service .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /order-service /order-service

EXPOSE 50051 9091
ENTRYPOINT ["/order-service"]
