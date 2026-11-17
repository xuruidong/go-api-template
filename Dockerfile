# 多阶段构建：编译阶段包含 Go 工具链，运行阶段只保留二进制。
# 最终镜像基于 distroless，无 shell、无包管理器，攻击面最小。

FROM golang:1.25 AS build

WORKDIR /src

# 先复制依赖清单，利用 layer 缓存：只有依赖变化时才重新下载模块。
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
# CGO_ENABLED=0 生成静态链接二进制，才能直接跑在 distroless/static 上。
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/server ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=build /out/server /app/server

# distroless nonroot 镜像默认以 UID 65532 运行。
USER nonroot:nonroot

ENV HTTP_ADDR=:8080 \
    APP_ENV=production

EXPOSE 8080

ENTRYPOINT ["/app/server"]
