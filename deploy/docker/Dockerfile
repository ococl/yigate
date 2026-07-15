# 构建阶段
FROM golang:1.21-alpine AS builder
WORKDIR /build

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY src/go.mod src/go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download

COPY src/*.go ./
COPY src/proxy/ ./proxy/
COPY deploy/docker/entrypoint.sh ./entrypoint.sh

RUN CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct \
    go build -trimpath -mod=readonly \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o yigate .

# 运行阶段
FROM alpine:3.20
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/yi-team/yigate" \
      org.opencontainers.image.created="${BUILD_TIME}"

RUN apk add --no-cache ca-certificates && \
    adduser -D -u 1000 yigate
WORKDIR /k5
COPY --from=builder /build/yigate /usr/local/bin/yigate
COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    mkdir -p /var/lib/yigate/config /var/lib/yigate/data /var/log/yigate && \
    chown -R yigate:yigate /var/lib/yigate /var/log/yigate
USER yigate
EXPOSE 8891
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
