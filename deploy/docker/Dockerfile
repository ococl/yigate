# 前端构建阶段
FROM node:26.13.0-alpine AS webui
WORKDIR /webui
COPY web/ ./web/
RUN cd web && npm ci && npm run build && node scripts/embed-dist.mjs dist /webui/webui_dist

# 构建阶段
FROM golang:1.27.3-alpine AS builder
WORKDIR /build

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

COPY src/go.mod src/go.sum ./src/
RUN cd src && GOPROXY=https://goproxy.cn,direct go mod download

COPY src/ ./src/

# 将压缩后的前端产物拷贝到 Go embed 目录（webui_dist 已被 .gitignore，仅构建期存在）。
# 注意：必须在 COPY src/ 之后执行，否则 src 中占位的 index.html 会覆盖真实前端入口。
COPY --from=webui /webui/webui_dist ./src/proxy/webui_dist

COPY deploy/docker/entrypoint.sh ./entrypoint.sh
RUN cd src && CGO_ENABLED=0 GOOS=linux GOPROXY=https://goproxy.cn,direct \
    go build -trimpath -mod=readonly \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.buildTime=${BUILD_TIME}" \
    -o /build/yigate .

# 运行阶段
FROM alpine:3.24.4
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_TIME=unknown

LABEL org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}" \
      org.opencontainers.image.source="https://cnb.cool/yi.team/yigate" \
      org.opencontainers.image.created="${BUILD_TIME}"

RUN apk add --no-cache ca-certificates wget util-linux && \
    adduser -D -u 1000 yigate
WORKDIR /var/lib/yigate
COPY --from=builder /build/yigate /usr/local/bin/yigate
COPY --from=builder /build/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh && \
    mkdir -p /var/lib/yigate/data /var/lib/yigate/logs && \
    chown -R yigate:yigate /var/lib/yigate
EXPOSE 8891 8890
# 探活只证明管理端口在响应：/health 匿名且只回纯文本 UP，无需读取配置里的令牌。
# 代理是否可用是另一回事，看需认证的 /api/v1/status；端口被改动时可在 compose 中覆盖本检查。
HEALTHCHECK --interval=30s --timeout=5s --retries=3 --start-period=10s \
  CMD ["/usr/bin/wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8890/health"]
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
