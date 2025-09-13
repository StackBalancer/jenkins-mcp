FROM docker.io/golang:1.23-alpine AS builder
WORKDIR /src
COPY . .
RUN go env -w GOPROXY=https://proxy.golang.org && \
    go mod download && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /bin/mcp-server .

#FROM gcr.io/distroless/static-debian12
# Debug/runtime stage
FROM ubuntu:24.04
RUN apt-get update && apt-get install -y curl iputils-ping ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /bin/mcp-server /bin/mcp-server
EXPOSE 8081
ENTRYPOINT ["/bin/mcp-server"]
