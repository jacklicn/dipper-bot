# Build stage
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o dipper-bot .

# Runtime stage
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /app/dipper-bot .

# Create config directory
RUN mkdir -p /root/.dipper-bot

# Gateway default port
EXPOSE 8090

ENTRYPOINT ["./dipper-bot"]
CMD ["status"]
