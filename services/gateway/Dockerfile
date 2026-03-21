FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy workspace files
COPY go.work go.work.sum ./

# Copy all modules (needed for workspace resolution)
COPY pkg/ pkg/
COPY gen/ gen/
COPY services/gateway/ services/gateway/

# Download dependencies
WORKDIR /app/services/gateway
RUN go mod download

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /gateway .

# Runtime
FROM alpine:3.20
RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /gateway /gateway

EXPOSE 8080
ENTRYPOINT ["/gateway"]
