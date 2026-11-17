package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5/middleware"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

// requestValidator 构造 OpenAPI 请求校验中间件。
//
// 它做的事情：
//   - 路径/方法必须出现在文档中，否则 404/405
//   - 路径参数、查询参数、请求头的类型与格式校验、默认值填充
//   - 请求体 JSON Schema 校验（必填字段、长度、取值范围、additionalProperties 等）
//   - 依据文档中的 security 要求调用 s.authenticate 完成鉴权
//
// 参数校验因此不需要在 handler 里重复编写，handler 可以信任参数已合法。
func (s *Server) requestValidator(spec *openapi3.T) (func(http.Handler) http.Handler, error) {
	return nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		// 只对“路径”做匹配，不校验 Host 头。
		// 否则 localhost:8080 与线上域名不一致时，所有请求都会被判为“找不到路由”。
		DoNotValidateServers: true,

		Options: openapi3filter.Options{
			AuthenticationFunc: s.authenticate,
		},

		// 统一把校验失败转成项目约定的 Problem JSON，而不是 http.Error 的纯文本。
		ErrorHandlerWithOpts: s.validationErrorHandler,
	}), nil
}

// validationErrorHandler 把校验中间件的错误映射为统一错误响应。
//
// 中间件在 opts.StatusCode 中给出建议状态码：
// 401 鉴权失败、400 请求不合法、404 路径不存在、405 方法不允许、500 校验器自身异常。
func (s *Server) validationErrorHandler(
	_ context.Context, err error, w http.ResponseWriter, r *http.Request, opts nethttpmiddleware.ErrorHandlerOpts,
) {
	switch opts.StatusCode {
	case http.StatusUnauthorized:
		writeProblem(w, r, http.StatusUnauthorized, "unauthorized", "Unauthorized", "缺少或无效的认证信息")
	case http.StatusNotFound:
		writeProblem(w, r, http.StatusNotFound, "not_found", "Not Found", "请求的路径未在 API 文档中定义")
	case http.StatusMethodNotAllowed:
		writeProblem(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method Not Allowed", "该路径不支持此 HTTP 方法")
	case http.StatusBadRequest:
		// 校验器返回的错误往往包含完整的 JSON Schema 片段（多行），
		// 对调用方只回第一行（通常是 "parameter "x" in query has an error: ..." 这类摘要），
		// 完整内容留在 debug 日志里便于排查。
		s.logger.Debug("请求不合法", slog.String("path", r.URL.Path), slog.Any("error", err))
		writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Bad Request", firstLine(err.Error()))
	default:
		// 校验器内部异常：对外只给笼统信息，细节进日志。
		s.logger.Error("请求校验失败", slog.String("path", r.URL.Path), slog.Any("error", err))
		writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "服务内部错误")
	}
}

// accessLog 记录每个请求的关键信息。
//
// 使用结构化日志（JSON），便于采集到 ELK / Loki 后按 request_id 串联排障。
func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		level := slog.LevelInfo
		if ww.Status() >= http.StatusInternalServerError {
			level = slog.LevelError
		}

		s.logger.LogAttrs(r.Context(), level, "http request",
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", ww.Status()),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Duration("duration", time.Since(start)),
			slog.String("remote_addr", r.RemoteAddr),
			slog.String("user_agent", r.UserAgent()),
		)
	})
}

// recoverPanic 兜底捕获 handler panic。
//
// 没有它，一个 nil 指针就会让整个进程退出；
// 有了它，返回 500 并记录堆栈，服务继续处理其他请求。
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// 客户端主动断开连接属于正常现象，不当作服务端错误。
				if errors.Is(asError(rec), http.ErrAbortHandler) {
					panic(rec)
				}
				s.logger.Error("panic recovered",
					slog.String("request_id", middleware.GetReqID(r.Context())),
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
				)
				writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "服务内部错误")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func asError(v any) error {
	if err, ok := v.(error); ok {
		return err
	}
	return nil
}

// firstLine 取错误信息的第一行，避免把整段 schema 定义回显给调用方。
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
