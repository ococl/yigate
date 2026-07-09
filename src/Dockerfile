# 构建阶段
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o k5pw .

# 运行阶段
FROM alpine:3.20
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 k5pw
WORKDIR /k5
COPY --from=builder /build/k5pw /usr/local/bin/k5pw
COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    mkdir -p /k5/logs && \
    chown -R k5pw:k5pw /k5
USER k5pw
EXPOSE 8891
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
