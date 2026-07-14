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
    -o k5pw .

# 运行阶段
FROM alpine:3.20
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://github.com/yi-team/k5pw" \
      org.opencontainers.image.created="${BUILD_TIME}"

RUN apk add --no-cache ca-certificates wget && \
    adduser -D -u 1000 k5pw
WORKDIR /k5
COPY --from=builder /build/k5pw /usr/local/bin/k5pw
COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    mkdir -p /var/lib/k5pw/config /var/lib/k5pw/data /var/log/k5pw && \
    chown -R k5pw:k5pw /var/lib/k5pw /var/log/k5pw
USER k5pw
EXPOSE 8891
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
