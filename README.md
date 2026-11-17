# go-api-template

基于 [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) 的 Go 服务端开发模板：
**以 OpenAPI 文档为唯一契约**，路由、参数绑定、请求校验、数据模型、类型化客户端全部由代码生成，
手写代码只负责业务逻辑。

开箱即用的部分：

| 能力 | 实现 |
| --- | --- |
| API 契约优先 | `api/openapi.yaml` → 生成服务端接口、模型、客户端 |
| 严格模式处理器 | `strict-server`：强类型 `XxxRequestObject` 进、`XxxResponseObject` 出 |
| 请求校验 | `oapi-codegen/nethttp-middleware` + `kin-openapi`，按文档校验参数/请求体/鉴权 |
| 路由 | chi（`chi-server`） |
| 错误响应 | 统一的 Problem JSON（RFC 9457 风格） |
| 鉴权 | OpenAPI `security` 驱动，示例为 Bearer Token，可替换为 JWT/OIDC |
| 可观测性 | 结构化日志（slog）、request id、访问日志、panic 兜底 |
| 生命周期 | 优雅关闭、HTTP 超时、fail-fast 配置校验 |
| 测试 | store 单测 + 真实 TCP 集成测试（复用生成的客户端） |
| 交付 | Makefile、多阶段 Dockerfile（distroless）、GitHub Actions |

---

## 1. 目录结构

```
.
├── api/
│   └── openapi.yaml            # ★ 唯一契约：所有接口、模型、错误、鉴权都在这里定义
├── cmd/
│   └── server/main.go          # 程序入口：配置 → 依赖装配 → 启动 → 优雅关闭
├── internal/
│   ├── api/
│   │   ├── cfg.yaml            # oapi-codegen 服务端生成配置
│   │   ├── generate.go         # go:generate 指令
│   │   └── server.gen.go       # 【生成】路由、接口、模型、内嵌 spec（勿手改）
│   ├── config/config.go        # 环境变量加载与校验
│   ├── server/
│   │   ├── server.go           # 路由与中间件装配、健康检查
│   │   ├── middleware.go       # 请求校验中间件、访问日志、panic 恢复
│   │   ├── auth.go             # Bearer 鉴权 + 身份注入 context
│   │   ├── problems.go         # 统一错误类型与 Problem JSON 输出
│   │   ├── todo.go             # ★ 业务 handler（示例）
│   │   └── integration_test.go # 集成测试（真实 HTTP + 生成的客户端）
│   └── store/
│       ├── store.go            # 领域模型与 Store 接口
│       ├── memory.go           # 内存实现（示例/测试用）
│       └── memory_test.go
├── pkg/client/
│   ├── cfg.yaml                # oapi-codegen 客户端生成配置
│   ├── generate.go
│   └── client.gen.go           # 【生成】类型化 HTTP 客户端（勿手改）
├── docs/
│   └── extending.md            # 扩展指南：换数据库、加接口、拆分模块、替换鉴权
├── Makefile
├── Dockerfile
└── .github/workflows/ci.yml
```

四层分工，改动时各归各位：

| 层 | 位置 | 你该改什么 |
| --- | --- | --- |
| 契约层 | `api/openapi.yaml` | 增删接口、改字段、改校验规则 |
| 生成层 | `*.gen.go` | **永远不要手改**，由 `make generate` 覆盖 |
| 业务层 | `internal/server`、`internal/store` | handler 实现、领域逻辑、错误映射 |
| 装配层 | `cmd/server/main.go`、`internal/config` | 依赖注入、启动参数、基础设施接入 |

---

## 2. 快速开始

### 2.1 前置条件

- **Go 1.25+**（`oapi-codegen` v2.8 要求 1.25；生成代码本身只要求 1.24+）
- `make`、`git`、`curl`：Windows 上请使用 **Git Bash** 或 WSL（PowerShell 不提供 `make`）

```bash
go version   # go version go1.25.x ...
```

### 2.2 跑起来

```bash
# 1) 生成服务端 + 客户端代码（oapi-codegen 已由 go.mod 的 tool 指令固定版本，无需单独安装）
make generate

# 2) 跑测试（含真实 HTTP 集成测试）
make test

# 3) 启动服务（自动注入开发用 AUTH_TOKEN=dev-token-12345）
make run
```

另开一个终端验证：

```bash
# 公开接口，无需 Token
curl -s http://localhost:8080/healthz
# {"status":"ok","version":"dev"}

# 需要鉴权
curl -s http://localhost:8080/todos -H "Authorization: Bearer dev-token-12345"
# {"items":[],"limit":20,"offset":0,"total":0}

# 创建
curl -s -X POST http://localhost:8080/todos \
  -H "Authorization: Bearer dev-token-12345" \
  -H "Content-Type: application/json" \
  -d '{"title":"学习 oapi-codegen"}'
# {"completed":false,"createdAt":"...","id":1,"title":"学习 oapi-codegen","updatedAt":"..."}

# 参数不合法 -> 400 Problem JSON
curl -s "http://localhost:8080/todos?limit=abc" -H "Authorization: Bearer dev-token-12345"

# 未认证 -> 401
curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/todos

# 运行时拿到内嵌的 OpenAPI 文档，可直接导入 Postman / Swagger UI
curl -s http://localhost:8080/openapi.json | head -c 200
```

### 2.3 移植到自己的项目

```bash
# 换成你自己的模块名（会同步更新所有 import）
go mod edit -module github.com/yourorg/your-service
grep -rl 'github.com/example/go-api-template' --include='*.go' . | xargs sed -i 's#github.com/example/go-api-template#github.com/yourorg/your-service#g'

# 改 API 标题、版本、接口
# 编辑 api/openapi.yaml

make generate && make test && make run
```

---

## 3. API 一览

| 方法 | 路径 | operationId | 鉴权 | 说明 |
| --- | --- | --- | --- | --- |
| GET | `/healthz` | `getHealth` | 否 | 健康检查（存活探针） |
| GET | `/todos` | `listTodos` | 是 | 列表：`limit`/`offset`/`completed`/`q` |
| POST | `/todos` | `createTodo` | 是 | 创建，201 + `Location` 头 |
| GET | `/todos/{id}` | `getTodo` | 是 | 详情 |
| PATCH | `/todos/{id}` | `updateTodo` | 是 | 局部更新（`minProperties: 1`） |
| DELETE | `/todos/{id}` | `deleteTodo` | 是 | 删除，204 |
| GET | `/openapi.json` | — | 否 | 内嵌 OpenAPI 文档（不参与校验） |

约定：

- 所有非 2xx 响应体都是 `Problem` 结构：`type` / `title` / `status` / `detail` / `instance` / `code`。
- `code` 是稳定的机器可读错误码（`not_found`、`invalid_request`、`unauthorized` 等），客户端据此分支处理；
  用 `detail` 展示给人看。
- 时间一律 RFC 3339（UTC）；`Todo` 的 `id` 由服务端生成。

---

## 4. 核心工作流：改接口

> **黄金法则：先改 `api/openapi.yaml`，再 `make generate`，让编译器告诉你哪里需要改。**

举例：给 `Todo` 增加 `priority` 字段，并新增一个接口。

```bash
# 1) 编辑契约
vim api/openapi.yaml

# 2) 重新生成
make generate

# 3) 编译（或直接 go build ./...），编译器指出所有待改位置
make build
```

第 3 步的编译错误就是完整的待办清单，例如：

```
internal/server/todo.go:130:4: cannot use t.Priority (variable of type *int32)
    as type *int32 in struct literal  // 领域模型还没这个字段
```

### 加一个新接口的完整步骤

1. **写契约**：在 `api/openapi.yaml` 的 `paths` 下新增 path，指定 `operationId`
   （生成的方法名就是 `operationId` 的驼峰形式，所以起名要语义化：`archiveTodo` → `ArchiveTodo`）。
2. **生成**：`make generate`。
3. **实现**：`internal/api/server.gen.go` 中的 `StrictServerInterface` 多了个方法；
   在 `internal/server/` 下实现它，签名形如：

   ```go
   func (s *Server) ArchiveTodo(ctx context.Context, request api.ArchiveTodoRequestObject) (api.ArchiveTodoResponseObject, error) {
       // 返回 nil, err 时，ResponseErrorHandlerFunc 统一转 Problem JSON；
       // 返回强类型响应则自动设置状态码与 Content-Type。
       return api.ArchiveTodo200JSONResponse(...), nil
   }
   ```

4. **测试**：集成测试里用生成的客户端调用，编译期即可发现契约变更导致的测试失效。

### 校验规则写在文档里，不要写进 handler

由于挂载了请求校验中间件，下面这些**都不用写代码**：

```yaml
title:
  type: string
  minLength: 1      # 空字符串 → 400
  maxLength: 200    # 超长 → 400
parameters:
  - name: limit
    in: query
    schema:
      type: integer
      minimum: 1
      maximum: 100
      default: 20   # 未传时由中间件填默认值
```

请求到达 handler 时，参数已通过类型、范围、必填、`additionalProperties` 等全部校验。
在 handler 里再做一次同样的判断属于重复逻辑。

### 生成的命名约定

配置里统一启用了 `name-normalizer: ToCamelCaseWithInitialisms`（`internal/api/cfg.yaml`
与 `pkg/client/cfg.yaml` 必须一致），因此：

- `id` → `ID`，`httpUrl` → `HTTPURL`
- operationId `listTodos` + 200 + JSON → `ListTodos200JSONResponse`
- 错误响应组件 `Unauthorized` → 每个 operation 各自的 `Xxx401JSONResponse`

服务端与客户端使用同一个 normalizer，否则同一字段会出现 `Id` / `ID` 两种写法。

---

## 5. 请求生命周期

```
HTTP 请求
   │
   ├─ recoverPanic        panic → 500 Problem，进程不退出
   ├─ RequestID           注入 X-Request-Id，贯穿日志
   ├─ RealIP              识别真实客户端 IP
   ├─ accessLog           记录 method/path/status/耗时
   │
   ├─ requestValidator    ← oapi-codegen/nethttp-middleware
   │     ├─ 路径/方法不在文档中 → 404 / 405
   │     ├─ 参数、请求体不符合 schema → 400
   │     └─ security 不满足 → 调用 authenticate → 401
   │
   ├─ principalMiddleware 把已认证身份写入 context
   │
   ├─ 生成的路由 wrapper  chi 路由 → 参数绑定
   ├─ strictHandler       请求体解码 → XxxRequestObject
   │
   └─ Server.XxxHandler   业务逻辑 → XxxResponseObject
         │
         └─ 返回 error → responseErrorHandler → Problem JSON（或 500）
```

两条"出口"容易混淆，分工如下：

| 位置 | 处理什么 | 文件 |
| --- | --- | --- |
| `validationErrorHandler` | 校验中间件拦截的请求（在业务代码之前） | `middleware.go` |
| `requestErrorHandler` | strict server 解析请求体失败 | `problems.go` |
| `responseErrorHandler` | handler 返回的 `*APIError` / 未知错误 | `problems.go` |
| `r.NotFound` / `MethodNotAllowed` | 连 chi 路由都没匹配上 | `server.go` |

---

## 6. 配置

全部来自环境变量，见 `.env.example`。**模板不读取 `.env` 文件**（部署平台注入即可）。

| 变量 | 必填 | 默认 | 说明 |
| --- | --- | --- | --- |
| `AUTH_TOKEN` | ✅ | — | Bearer Token，≥ 8 字符；缺失时进程启动失败 |
| `HTTP_ADDR` | | `:8080` | 监听地址 |
| `APP_ENV` | | `development` | `development` 用文本日志，其他值用 JSON |
| `LOG_LEVEL` | | `info` | `debug` / `info` / `warn` / `error` |
| `SHUTDOWN_TIMEOUT` | | `10s` | 优雅关闭最长等待 |
| `READ_HEADER_TIMEOUT` | | `5s` | 防慢速攻击 |
| `READ_TIMEOUT` | | `15s` | |
| `WRITE_TIMEOUT` | | `30s` | |
| `IDLE_TIMEOUT` | | `60s` | |

设计取舍：配置校验放在启动阶段（fail fast），错误配置的进程不会对外提供服务。

---

## 7. 测试

```bash
make test          # 全部测试（几秒内跑完）
make cover         # 覆盖率 → coverage.html
make test-race     # 数据竞争检测（需 CGO + C 编译器，建议在 CI/Linux 执行）
```

测试策略：

- `internal/store/memory_test.go`：纯单元测试，覆盖 CRUD、过滤、分页、context 取消。
- `internal/server/integration_test.go`：`httptest.NewServer` 起真实 TCP 服务，
  **用 `pkg/client` 里生成的客户端**发起请求。这样测试本身也在验证客户端生成结果：
  契约一改，测试要么编译失败、要么断言失败，不会悄悄失效。

集成测试覆盖了：CRUD 全流程、`Location` 头、分页与过滤、参数校验 400、
空 PATCH 体 400、无 Token / 错 Token 401、公开接口免鉴权、未知路径 404、`/openapi.json`。

---

## 8. 构建与部署

```bash
make build                    # → bin/go-api-template，版本号来自 git describe
make docker-build             # 多阶段构建，运行镜像为 distroless/static:nonroot
make docker-run               # 本地跑容器
make smoke                    # 启动二进制并做一轮冒烟（需先 make build）
```

- `Dockerfile` 采用多阶段构建：编译产物为静态链接二进制，运行镜像无 shell、无包管理器。
- 版本号通过 `-ldflags "-X main.version=..."` 注入，出现在 `/healthz` 与启动日志中。
- 服务监听 SIGTERM 后停止接收新请求，等待在途请求完成（滚动更新不丢请求）。
- CI（`.github/workflows/ci.yml`）依次执行：生成代码一致性 → 格式 → vet → 测试（含 race）→ 构建。
  其中 `make generate-check` 会阻止"改了 `openapi.yaml` 却忘了提交生成代码"的 PR。

部署到 K8s 时的探针建议：

```yaml
livenessProbe:  { httpGet: { path: /healthz, port: 8080 } }
readinessProbe: { httpGet: { path: /healthz, port: 8080 } }
```

`/healthz` 刻意不探测下游依赖（数据库等），只表示"进程能处理请求"。需要就绪探针时，
单独实现 `/readyz` 检查数据库连接，不要复用 liveness——否则数据库抖动会触发容器重启风暴。

---

## 9. 依赖与版本

| 依赖 | 版本 | 说明 |
| --- | --- | --- |
| Go | 1.25+ | |
| `oapi-codegen` | v2.8.2 | 通过 `go.mod` 的 `tool` 指令管理 |
| `oapi-codegen/runtime` | v1.8.0 | 生成代码的运行时（v1.9 要求 Go 1.26，暂不升） |
| `nethttp-middleware` | v1.3.1 | 请求校验 + 鉴权 |
| `kin-openapi` | v0.152.0 | OpenAPI 解析与校验（需 ≥ 0.151 才能与中间件配合去重解码） |
| `chi` | v5.5.0 | 路由 |

升级生成器（`go.mod` 与两个 `cfg.yaml` 顶部的 schema URL 一起改）：

```bash
go get -tool github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.9.0
go mod tidy
make generate && make test
```

生成器版本必须提交在 `go.mod` 里（本模板已用 `tool` 指令固定），
这样 CI 与本地生成结果一致，避免"我这跑得通"型问题。

---

## 10. 常见问题

**Q：为什么直接 `go run ./cmd/server` 报 `AUTH_TOKEN 未设置`？**

环境变量必填，这是有意的 fail-fast。用 `make run`（已注入开发 Token），或自行 `export AUTH_TOKEN=...`。

**Q：改了 `openapi.yaml`，为什么请求还是 404？**

生成代码里的路由来自编译时嵌入的 spec，必须先 `make generate` 并重新编译。运行中的进程不会热加载。

**Q：为什么校验器拒绝了某个请求，但 handler 里明明没写校验？**

这正是设计目标：校验规则全在文档里（`minLength`、`maxLength`、`minimum`、`additionalProperties` 等）。
改规则请改 `api/openapi.yaml`。

**Q：如何让某个接口完全公开？**

在该 operation 下写 `security: []`（覆盖全局 `security`），如 `getHealth` 所示。不需要改任何 Go 代码。

**Q：生成代码要提交吗？**

要。好处是：CI 无需安装生成器即可编译、PR diff 里能直接看到契约变更对代码的影响、
`make generate-check` 能校验一致性。这也是 oapi-codegen 官方 FAQ 的推荐做法。

**Q：字段为什么是 `Id` 而不是 `ID`？**

说明该生成配置没开 `name-normalizer: ToCamelCaseWithInitialisms`。务必让服务端与客户端两个 `cfg.yaml` 保持一致。

**Q：能换 gin/echo/std-http 吗？**

可以。把 `internal/api/cfg.yaml` 里的 `chi-server` 换成 `gin-server` / `echo-server` / `std-http-server`，
再把 `HandlerFromMux` 相关装配按对应 router 调整（见 oapi-codegen `docs/` 下各 server 文档）。
**业务 handler 与 store 层完全不用动**，因为 strict server 的接口签名与 router 无关。

**Q：想接入数据库怎么做？**

实现 `internal/store.Store` 接口，在 `cmd/server/main.go` 里替换 `store.NewMemory()`。
参考 `docs/extending.md`。

---

## 11. 延伸阅读

- [`docs/extending.md`](docs/extending.md)：接 Postgres、替换鉴权为 JWT、拆分多模块 API、加中间件
- [oapi-codegen README](https://github.com/oapi-codegen/oapi-codegen)：生成器全部能力
- [oapi-codegen configuration-schema.json](https://github.com/oapi-codegen/oapi-codegen/blob/main/configuration-schema.json)：配置项全集（`cfg.yaml` 顶部已挂 schema，IDE 有补全）
- [nethttp-middleware](https://github.com/oapi-codegen/nethttp-middleware)：校验中间件的可选项
