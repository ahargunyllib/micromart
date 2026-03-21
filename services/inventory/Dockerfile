FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY . .

WORKDIR /app/services/inventory
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /inventory-service .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /inventory-service /inventory-service

EXPOSE 50052 9092
ENTRYPOINT ["/inventory-service"]
