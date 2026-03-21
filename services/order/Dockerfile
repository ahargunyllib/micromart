FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY . .

WORKDIR /app/services/order
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /order-service .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /order-service /order-service

EXPOSE 50051 9091
ENTRYPOINT ["/order-service"]
