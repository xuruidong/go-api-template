# 本 Makefile 面向 Git Bash / WSL / macOS / Linux。
# Windows 用户请在 Git Bash 中执行（PowerShell 不支持 make）。
# 也可以用 `make help` 查看全部目标。

SHELL := /bin/sh

APP        := go-api-template
MODULE     := $(shell go list -m)
CMD        := ./cmd/server
BIN_DIR    := bin
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS    := -s -w -X main.version=$(VERSION)

# 本地开发用的假 Token；生产环境请通过部署平台注入真实值。
DEV_TOKEN  ?= dev-token-12345
HTTP_ADDR  ?= :8080
DOCKER_TAG ?= $(APP):$(VERSION)

.PHONY: help
help: ## 显示可用目标
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: generate
generate: ## 由 api/openapi.yaml 重新生成服务端与客户端代码
	go generate ./...

.PHONY: build
build: ## 编译二进制到 bin/
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP) $(CMD)
	@echo "built $(BIN_DIR)/$(APP) ($(VERSION))"

.PHONY: run
run: ## 本地启动服务（自动注入开发用 AUTH_TOKEN）
	AUTH_TOKEN=$(DEV_TOKEN) HTTP_ADDR=$(HTTP_ADDR) APP_ENV=development go run $(CMD)

.PHONY: test
test: ## 运行全部测试（含真实 HTTP 的集成测试）
	go test -count=1 ./...

.PHONY: test-race
test-race: ## 带数据竞争检测运行测试（需要 CGO 与 C 编译器，建议在 CI/Linux 上跑）
	CGO_ENABLED=1 go test -race -count=1 ./...

.PHONY: cover
cover: ## 生成覆盖率报告 coverage.out / coverage.html
	go test -count=1 -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1
	go tool cover -html=coverage.out -o coverage.html
	@echo "report: coverage.html"

.PHONY: vet
vet: ## go vet 静态检查
	go vet ./...

.PHONY: fmt
fmt: ## 格式化代码
	gofmt -l -w .

.PHONY: fmt-check
fmt-check: ## 检查格式（CI 使用，有差异则失败）
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "以下文件未格式化:"; echo "$$out"; exit 1; fi

.PHONY: tidy
tidy: ## 整理 go.mod / go.sum
	go mod tidy

.PHONY: check
check: fmt-check vet test ## 提交前跑一遍：格式 + 静态检查 + 测试

.PHONY: generate-check
generate-check: ## 校验生成代码与 OpenAPI 文档是否同步（CI 使用）
	@if ! git rev-parse --git-dir >/dev/null 2>&1; then \
		echo "跳过：当前目录不是 git 仓库"; exit 0; \
	fi; \
	go generate ./...; \
	if ! git diff --exit-code -- '*.gen.go' >/dev/null 2>&1; then \
		echo "生成代码与 api/openapi.yaml 不一致，请执行 make generate 并提交结果"; \
		git --no-pager diff --stat -- '*.gen.go'; exit 1; \
	fi; \
	echo "生成代码已是最新"

.PHONY: smoke
smoke: ## 启动服务并做一轮冒烟测试（需先 make build）
	@AUTH_TOKEN=$(DEV_TOKEN) $(BIN_DIR)/$(APP) & \
	pid=$$!; trap "kill $$pid 2>/dev/null" EXIT; sleep 1; \
	echo "--- GET /healthz"; \
	curl -fsS http://localhost$(HTTP_ADDR)/healthz; echo; \
	echo "--- POST /todos"; \
	curl -fsS -X POST http://localhost$(HTTP_ADDR)/todos \
		-H "Authorization: Bearer $(DEV_TOKEN)" -H "Content-Type: application/json" \
		-d '{"title":"smoke test"}'; echo; \
	echo "--- 未认证请求应返回 401"; \
	curl -s -o /dev/null -w "%{http_code}\n" http://localhost$(HTTP_ADDR)/todos

.PHONY: docker-build
docker-build: ## 构建镜像
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_TAG) .

.PHONY: docker-run
docker-run: ## 运行镜像
	docker run --rm -p 8080:8080 -e AUTH_TOKEN=$(DEV_TOKEN) $(DOCKER_TAG)

.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(BIN_DIR) coverage.out coverage.html
