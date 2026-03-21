FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY . .

WORKDIR /app/services/gateway
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway .

FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /gateway /gateway

EXPOSE 8080
ENTRYPOINT ["/gateway"]
