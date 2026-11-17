// Package server 组装 HTTP 服务：路由、中间件、鉴权与业务 handler。
//
// 包内的 server.gen.go（由 oapi-codegen 生成）与手写代码的分工：
//   - server.gen.go：路由注册、参数绑定、请求解码、响应序列化、模型定义
//   - 本包手写文件：中间件链、鉴权、错误格式、业务逻辑实现
//
// 每次 regenerate 只会覆盖 server.gen.go，手写文件保持不动。
package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/example/go-api-template/internal/api"
	"github.com/example/go-api-template/internal/store"
)

// Server 实现生成的 api.StrictServerInterface。
var _ api.StrictServerInterface = (*Server)(nil)

// Server 持有业务依赖，所有 handler 都是它的方法。
//
// 用显式依赖注入（而不是包级全局变量）的好处是：
// 测试里可以传入内存版 Store，生产里可以传入数据库实现。
type Server struct {
	store     store.Store
	logger    *slog.Logger
	authToken string
	version   string
}

// New 创建 Server。
func New(st store.Store, logger *slog.Logger, authToken, version string) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		store:     st,
		logger:    logger,
		authToken: authToken,
		version:   version,
	}
}

// Handler 组装完整的 http.Handler。
//
// 中间件顺序（外 -> 内）：
//
//	panic 恢复 -> RequestID -> RealIP -> 访问日志 -> OpenAPI 请求校验/鉴权 -> 身份注入 -> 业务 handler
//
// OpenAPI 请求校验中间件会拦截文档中不存在的路径（404）、
// 参数类型不合法（400）以及未通过鉴权的请求（401），
// 因此业务 handler 收到的数据已经过 schema 校验。
func (s *Server) Handler() (http.Handler, error) {
	spec, err := api.GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("加载内嵌 OpenAPI 文档失败: %w", err)
	}

	// strict 模式：把生成的路由包装成“强类型请求进、强类型响应出”的 handler，
	// 并接管请求解析失败与 handler 返回错误时的响应写出。
	strictHandler := api.NewStrictHandlerWithOptions(s, nil, api.StrictHTTPServerOptions{
		RequestErrorHandlerFunc:  s.requestErrorHandler,
		ResponseErrorHandlerFunc: s.responseErrorHandler,
	})

	validator, err := s.requestValidator(spec)
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	r.Use(s.recoverPanic)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(s.accessLog)

	// chi 的兜底处理器同样输出 Problem JSON，保证错误响应格式全局一致。
	// 未匹配到任何路由的请求不会进入下面的校验中间件，因此需要在这里单独处理。
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusNotFound, "not_found", "Not Found", "请求的路径不存在")
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed", "该路径不支持此 HTTP 方法")
	})

	// /openapi.json 不经过 OpenAPI 校验中间件（文档本身没有描述这个路径），
	// 因此注册在 group 之外。
	r.Get("/openapi.json", s.serveOpenAPISpec)

	r.Group(func(r chi.Router) {
		r.Use(validator)
		r.Use(s.principalMiddleware)
		// HandlerFromMux 把每条 operation 注册为 chi 路由；
		// 路由的路径、方法、参数修饰符全部来自 api/openapi.yaml。
		_ = api.HandlerFromMux(strictHandler, r)
	})

	return r, nil
}

// serveOpenAPISpec 返回内嵌的原始 OpenAPI 文档，便于调试、导入 Postman/文档站。
//
// 注意这里返回的是生成时嵌入的原始 JSON，而不是 GetSwagger() 解析后的对象，
// 因此与 api/openapi.yaml 内容一致。
func (s *Server) serveOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	raw, err := api.GetSpecJSON()
	if err != nil {
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "读取 OpenAPI 文档失败")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(raw)
}

// GetHealth 实现 GET /healthz。
//
// 健康检查刻意不做任何下游依赖探测：它只表示“进程还能处理 HTTP 请求”，
// 用于存活探针。若需要就绪探针（数据库可用性等），另开 /readyz 并单独实现。
func (s *Server) GetHealth(_ context.Context, _ api.GetHealthRequestObject) (api.GetHealthResponseObject, error) {
	return api.GetHealth200JSONResponse(api.Health{
		Status:  api.Ok,
		Version: s.version,
	}), nil
}

// errFromStore 把存储层错误翻译成对外 HTTP 错误。
//
// 这样存储实现（内存/Postgres/Redis）不需要知道 HTTP 语义。
func errFromStore(err error) error {
	if errors.Is(err, store.ErrNotFound) {
		return errNotFound("资源不存在")
	}
	return err
}
