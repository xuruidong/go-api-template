# 扩展指南

本模板只做到"能跑、能测、结构清晰"。这一篇讲怎么把它改造成你的业务服务。

## 1. 接入真实数据库

### 1.1 实现 Store 接口

`internal/store/store.go` 的 `Store` 接口是与 HTTP 层之间唯一的缝：

```go
type Store interface {
    Create(ctx context.Context, params CreateParams) (Todo, error)
    Get(ctx context.Context, id int64) (Todo, error)
    List(ctx context.Context, filter ListFilter) (items []Todo, total int64, err error)
    Update(ctx context.Context, id int64, patch Patch) (Todo, error)
    Delete(ctx context.Context, id int64) error
}
```

新建 `internal/store/postgres/postgres.go`：

```go
package postgres

import (
    "context"
    "database/sql"
    "errors"
    "fmt"

    "github.com/yourorg/your-service/internal/store"
)

type Store struct{ db *sql.DB }

var _ store.Store = (*Store)(nil)

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, p store.CreateParams) (store.Todo, error) {
    const q = `INSERT INTO todos (title, description, completed)
               VALUES ($1, $2, $3)
               RETURNING id, title, description, completed, created_at, updated_at`

    var t store.Todo
    err := s.db.QueryRowContext(ctx, q, p.Title, p.Description, p.Completed).
        Scan(&t.ID, &t.Title, &t.Description, &t.Completed, &t.CreatedAt, &t.UpdatedAt)
    if err != nil {
        return store.Todo{}, fmt.Errorf("insert todo: %w", err)
    }
    return t, nil
}

func (s *Store) Get(ctx context.Context, id int64) (store.Todo, error) {
    // ...
    if errors.Is(err, sql.ErrNoRows) {
        return store.Todo{}, store.ErrNotFound   // ← 必须映射为 store.ErrNotFound
    }
    // ...
}
```

**关键约定**：存储层只返回 `store.ErrNotFound` 或包装后的原始错误；
把它翻译成 HTTP 404 是 `internal/server.errFromStore` 的职责。
这样换存储不影响 HTTP 语义。

钢性要求：所有方法尊重 `ctx`（用 `QueryContext`/`ExecContext`），
否则超时和客户端断开会把数据库连接耗光。

### 1.2 装配

```go
// cmd/server/main.go
db, err := sql.Open("pgx", cfg.DatabaseURL)
if err != nil { return fmt.Errorf("连接数据库: %w", err) }

db.SetMaxOpenConns(25)
db.SetMaxIdleConns(25)
db.SetConnMaxLifetime(5 * time.Minute)

st := postgres.New(db)
defer db.Close()

srv := server.New(st, logger, cfg.AuthToken, version)
```

数据库连接串同样从环境变量读（加到 `internal/config`，与 `AUTH_TOKEN` 一样在启动时校验）。

### 1.3 加迁移

模板不含迁移工具，推荐任选其一：

- [golang-migrate](https://github.com/golang-migrate/migrate)：`migrations/0001_create_todos.up.sql`
- [goose](https://github.com/pressly/goose)：可嵌入 `embed.FS`，在 `main` 里自动升级
- [atlas](https://atlasgo.io/)：从 Go 结构体或 SQL 生成迁移

**不要把迁移放在请求路径或 init 里**，用独立的 `cmd/migrate` 或在 CI/CD 的发布步骤执行。

---

## 2. 替换鉴权实现

当前是静态 Token，两处相关代码：

| 位置 | 职责 |
| --- | --- |
| `internal/server/auth.go: authenticate` | 决定"是否放行"，由校验中间件调用，scope 需求来自文档 |
| `internal/server/auth.go: principalMiddleware` | 把"谁"写进 context，供 handler 读取 |

换成 JWT 的示例（只改 `authenticate`，`principalMiddleware` 保持不变）：

```go
func (s *Server) authenticate(_ context.Context, input *openapi3filter.AuthenticationInput) error {
    if input.SecuritySchemeName != bearerSchemeName {
        return input.NewError(nil)
    }
    raw, ok := bearerToken(input.RequestValidationInput.Request)
    if !ok {
        return input.NewError(errors.New("missing bearer token"))
    }

    claims, err := s.jwtVerifier.Verify(raw)          // 校验签名、iss、aud、exp
    if err != nil {
        return input.NewError(err)                     // 中间件映射为 401
    }

    // scope 校验：文档里写的 security: [{bearerAuth: [todos:write]}] 会传到 input.Scopes
    for _, required := range input.Scopes {
        if !slices.Contains(claims.Scopes, required) {
            return input.NewError(fmt.Errorf("缺少 scope %q", required))
        }
    }
    return nil
}
```

要点：

- **不要在 handler 里再写鉴权**。scope 是契约的一部分，写在 OpenAPI 文档里，评审可见。
- `input.NewError(nil)` 会生成带 scheme 名的标准错误文本；包一层自建错误可以补充上下文。
- 401/403 的区分：目前中间件把 `SecurityRequirementsError` 统一映射为 401。
  若要区分"未认证 401 / 已认证但无权限 403"，在 `validationErrorHandler` 里用
  `errors.As(err, &securityErr)` 判断 `input.Scopes` 是否为空后选择状态码。
- 需要用户信息时，从 handler 的 `ctx` 取：

  ```go
  p, ok := server.PrincipalFromContext(ctx)
  ```

  （`authenticate` 拿不到返回给 `principalMiddleware` 的值，所以实际项目里通常把
  校验结果缓存到 `sync.Map[token]Principal` 或让 `principalMiddleware` 再解析一次——
  模板中演示的是后者的简化形态。）

---

## 3. 拆分多个 API 模块

单体到一定规模后，按业务域拆成多个 spec，各自生成到独立包。
利用 oapi-codegen 的 `import-mapping` 复用共享类型。

```
api/
├── common.yaml            # 共享 schema：Problem、分页参数、时间格式
├── todo.yaml              # /todos 相关
└── admin.yaml             # /admin 相关（引用 common 的 Problem）
internal/api/
├── common/  cfg.yaml → common.gen.go
├── todo/    cfg.yaml → todo.gen.go
└── admin/   cfg.yaml → admin.gen.go
```

`admin/cfg.yaml`：

```yaml
package: admin

generate:
  chi-server: true
  strict-server: true
  models: true

output: admin.gen.go

import-mapping:
  ../../api/common.yaml: github.com/yourorg/your-service/internal/api/common
```

规则：

- 被引用的 spec 也要生成（否则类型不存在）。
- 两个 spec 用 `strict-server` 时，`components/responses` 的跨文件 `$ref`
  要求被引用方**同样开启 `strict-server`**（否则 `<Name>JSONResponse` 未定义）。这是已知限制。
- 每个包独立挂载：

  ```go
  r.Route("/admin", func(r chi.Router) {
      _ = admin.HandlerFromMux(adminStrict, r)
  })
  ```

- 每个 spec 都需要自己的校验中间件（各自 `GetSwagger()`），
  按前缀注册，或写一个"按路径前缀选 spec"的组合中间件。

是否拆分：**接口数量超过 ~40 个、或由多个团队维护时**再拆。过早拆分会让跨包引用和部署变得更麻烦。

---

## 4. 新增中间件

chi 中间件链在 `internal/server/server.go: Handler()` 中装配，顺序即执行顺序（外 → 内）。

现成的、值得加的：

```go
import "github.com/go-chi/chi/v5/middleware"

r.Use(middleware.Timeout(30 * time.Second))                         // 请求超时
r.Use(middleware.ThrottleBacklog(100, 50, time.Second))             // 限流（简单版）
r.Use(middleware.Compress(5, "application/json"))                   // gzip
```

要注意的位置：

- **在 `validator` 之前**加的中间件能看到任何（包括非法）请求，适合限流、IP 黑名单、CORS。
- **在 `validator` 之后**加的中间件只处理合法请求，适合业务级日志、审计、幂等键。
- 如果中间件要提前结束请求，**必须用 `writeProblem` 输出**，否则响应格式与其他错误不一致。
- 生成的路由还支持 `ChiServerOptions.Middlewares`（`[]api.MiddlewareFunc`），
  那是挂在**每个 operation handler 外层**、在参数绑定之后的中间件；
  当前模板走 strict 模式，用 `api.NewStrictHandlerWithOptions(s, middlewares, ...)` 的
  `StrictMiddlewareFunc` 更合适——它还能拿到 operationID：

  ```go
  audit := func(next api.StrictHandlerFunc, opID string) api.StrictHandlerFunc {
      return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
          start := time.Now()
          resp, err := next(ctx, w, r, req)
          s.logger.InfoContext(ctx, "operation",
              slog.String("operation_id", opID),
              slog.Duration("duration", time.Since(start)),
              slog.Any("error", err),
          )
          return resp, err
      }
  }
  strictHandler := api.NewStrictHandlerWithOptions(s, []api.StrictMiddlewareFunc{audit}, opts)
  ```

  模板里目前传的是 `nil`，把 `audit` 加进去即可。

---

## 5. 生成代码的自定义

oapi-codegen 的输出可以用 `output-options.user-templates` 覆盖内置模板：

```yaml
output-options:
  user-templates:
    # 本地文件：相对 cfg.yaml 所在目录
    chi/chi-handler.tmpl: ./templates/chi-handler.tmpl
```

常见用途：

- 给所有 generated handler 加统一的注释头
- 改动 `HandlerWithOptions` 的注册方式（比如把所有路由挂到子 router）
- 客户端重试逻辑写在模板里而不是包一层

**代价**：模板基于生成器内部结构，升级 oapi-codegen 时可能失效。
除非确实需要，否则优先用 `compatibility:` 兼容选项或在生成代码外层做包装。

可用模板列表见 oapi-codegen 仓库 `pkg/codegen/templates/`。

---

## 6. 可观测性

模板已包含：结构化日志、request id、访问日志、panic 恢复。

要继续补齐：

**指标（Prometheus）**

```go
import "github.com/prometheus/client_golang/prometheus/promhttp"

r.Handle("/metrics", promhttp.Handler())
```

在 `accessLog` 里顺带记录 `http_requests_total{method,path,status}` 与耗时直方图即可。
注意把 path 换成**路由模式**（`/todos/{id}`）而不是真实路径，否则标签爆炸。

**链路追踪（OpenTelemetry）**

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

handler = otelhttp.NewHandler(handler, "http.server")
```

并让 `nethttp-middleware` 校验时复用 `r.Context()`（已经如此），span 会自动串联。

**日志关联**：`middleware.RequestID` 生成的 id 已放进 context 与日志；
在响应头补上它，方便用户报障时提供：

```go
r.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Request-Id", middleware.GetReqID(r.Context()))
        next.ServeHTTP(w, r)
    })
})
```

---

## 7. 错误处理约定

- 业务错误用 `server.NewAPIError(status, code, title, detail)` 返回，由 `responseErrorHandler` 转换。
  这种方式**只能**用于文档里已声明的状态码——否则客户端生成的响应类型里没有对应字段，
  调用方会拿到无法反序列化的响应。
- 需要返回文档未声明的状态码时（例如 409 冲突），先改 `api/openapi.yaml` 加上响应，再 `make generate`。
- 未知错误默认 500 并记录完整日志，对外只给笼统信息。**永远不要把 `err.Error()` 直接返回给调用方**
  （可能包含 SQL、路径、内部地址）。
- 中间件层错误用 `writeProblem`，保证 Content-Type 与 Problem 结构一致。

---

## 8. 检查清单：上线前

- [ ] `AUTH_TOKEN` 换成真实鉴权方案，删除静态 Token 代码
- [ ] 存储换成数据库实现，`sql.DB` 连接池与超时已配置
- [ ] 迁移脚本纳入发布流程，且可重复执行
- [ ] `APP_ENV=production`，日志为 JSON 并已接入采集
- [ ] `/healthz` 探针已配置；如需要就绪探测，新增 `/readyz`
- [ ] 加限流与请求超时中间件
- [ ] 编译进 `main.version`，`/healthz` 能确认线上版本
- [ ] CI 中 `make generate-check` 生效（防止契约与生成代码漂移）
- [ ] 错误响应与文档声明一致（尤其新增状态码时）
- [ ] 生成器版本固定在 `go.mod`，`cfg.yaml` 的 schema URL 与之匹配
