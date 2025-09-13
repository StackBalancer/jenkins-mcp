# 1. Build stage
FROM docker.io/golang:1.23-alpine AS builder

WORKDIR /app

# Copy go.mod and download deps first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the client code
COPY . .

# Build static binary
RUN CGO_ENABLED=0 GOOS=linux go build -o /mcp-client main.go

# 2. Runtime stage
FROM docker.io/alpine:3.19

RUN apk --no-cache add curl

WORKDIR /usr/local/bin

# Copy binary from builder
COPY --from=builder /mcp-client /usr/local/bin/mcp-client

# Environment variables (override at runtime)
ENV OPENAI_API_KEY=""
ENV MCP_SERVER_URL="http://mcp-server:8081/sse"

ENTRYPOINT ["mcp-client"]
