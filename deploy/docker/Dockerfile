# 构建阶段
FROM golang:1.21-alpine AS builder
WORKDIR /build

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

# 先构建前端（生成 web/dist）
COPY web/ ./web/
RUN cd web && \
    (command -v npm >/dev/null 2>&1 && npm install && npm run build) || \
    echo "warning: npm not available, webui will be empty"

# 将前端构建产物拷贝到 Go embed 目录
RUN mkdir -p src/proxy/webui_dist && \
    (test -d web/dist && cp -r web/dist/* src/proxy/webui_dist/ 2>/dev/null || true)

COPY src/go.mod src/go.sum ./src/
RUN cd src && GOPROXY=https://goproxy.cn,direct go mod download

COPY src/ ./src/
COPY deploy/docker/entrypoint.sh ./entrypoint.sh
RUN cd src && CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct \
    go build -trimpath -mod=readonly \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /build/yigate .

# 运行阶段
FROM alpine:3.20
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/yi-team/yigate" \
      org.opencontainers.image.created="${BUILD_TIME}"

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 1000 yigate
WORKDIR /k5
COPY --from=builder /build/yigate /usr/local/bin/yigate
COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    mkdir -p /var/lib/yigate/config /var/lib/yigate/data /var/log/yigate && \
    chown -R yigate:yigate /var/lib/yigate /var/log/yigate
USER yigate
EXPOSE 8891 8890
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
