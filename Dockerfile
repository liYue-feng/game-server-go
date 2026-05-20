# ========== 构建阶段 ==========
# 使用 Go 官方镜像编译项目
# 多阶段构建：编译产物最终拷贝到轻量级运行镜像，大幅减小镜像体积
FROM golang:1.23-alpine AS builder

# 安装编译依赖
# git: go mod 需要
# gcc/musl-dev: CGO 编译（MySQL 驱动需要）
RUN apk add --no-cache git gcc musl-dev

WORKDIR /app

# 先复制依赖文件，利用 Docker 缓存层
# 只要 go.mod / go.sum 不变，依赖层就不会重新构建
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY . .

# 编译
# CGO_ENABLED=0: 静态编译，不依赖 C 库，Alpine 镜像可运行
# -ldflags="-s -w": 去掉调试信息，减小二进制体积约 30%
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/bin/game-server ./cmd/server

# ========== 运行阶段 ==========
# scratch 是最小的 Docker 镜像（0字节基础层），只包含我们的二进制
FROM alpine:3.19

# 安装 CA 证书（HTTPS 请求需要，如微信 API）
RUN apk add --no-cache ca-certificates tzdata

# 设置时区为中国
ENV TZ=Asia/Shanghai

WORKDIR /app

# 从构建阶段拷贝编译产物
COPY --from=builder /app/bin/game-server .
COPY --from=builder /app/configs ./configs

# 暴露端口
# 8080: WebSocket 游戏服务
# 8081: HTTP 回调服务（微信支付回调）
EXPOSE 8080 8081

# 运行
ENTRYPOINT ["./game-server"]
CMD ["-config", "configs/config.yaml"]
