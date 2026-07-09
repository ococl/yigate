# 构建阶段
FROM golang:1.21-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o k5pw .

# 运行阶段
FROM alpine:latest
RUN apk add --no-cache ca-certificates
WORKDIR /k5
COPY --from=builder /build/k5pw /usr/local/bin/k5pw
EXPOSE 8891
ENTRYPOINT ["k5pw"]
CMD ["-config", "/k5/config.yml"]
