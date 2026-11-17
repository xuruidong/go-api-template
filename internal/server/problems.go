package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/example/go-api-template/internal/api"
)

// APIError 表示 handler 有意返回的业务错误。
//
// handler 返回 (nil, apiError) 时，由 ResponseErrorHandlerFunc 统一转换成
// RFC 9457（application/problem+json）格式；非 APIError 的错误一律记日志并返回 500，
// 避免把内部实现细节泄露给调用方。
type APIError struct {
	Status int    // HTTP 状态码
	Code   string // 机器可读错误码，例如 not_found
	Title  string // 简短概述
	Detail string // 详细说明（可安全展示）
}

// Error 让 APIError 满足 error 接口。
func (e *APIError) Error() string { return e.Code + ": " + e.Detail }

// NewAPIError 构造业务错误。
func NewAPIError(status int, code, title, detail string) *APIError {
	return &APIError{Status: status, Code: code, Title: title, Detail: detail}
}

// 常用错误构造器，供 handler 直接使用。
func errNotFound(detail string) *APIError {
	return NewAPIError(http.StatusNotFound, "not_found", "Not Found", detail)
}

func errBadRequest(detail string) *APIError {
	return NewAPIError(http.StatusBadRequest, "bad_request", "Bad Request", detail)
}

// newProblem 构造 API 契约中的 Problem 结构。
func newProblem(status int, code, title, detail, instance string) api.Problem {
	return api.Problem{
		Type:     "about:blank",
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &instance,
		Code:     &code,
	}
}

// writeProblem 直接向 ResponseWriter 输出 Problem JSON。
//
// 用于中间件层（参数校验失败、身份认证失败、panic 恢复等）——
// 这些位置拿不到 strict server 的强类型响应对象。
//
// Content-Type 使用 application/json：OpenAPI 文档把错误响应声明为 application/json，
// 实现与契约必须一致。若希望使用 RFC 9457 的 application/problem+json 媒体类型，
// 请先在 api/openapi.yaml 中把错误响应改为该类型，再重新生成代码。
func writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(newProblem(status, code, title, detail, r.URL.Path))
}

// requestErrorHandler 处理 strict server 在“解析请求”阶段的错误。
//
// 典型场景：JSON 语法错误、请求体类型不匹配、缺少必填字段、Content-Type 不支持。
// 这些都属于调用方的问题，统一返回 400 而不是 500。
func (s *Server) requestErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	s.logger.Debug("请求解析失败", slog.String("path", r.URL.Path), slog.Any("error", err))
	writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Bad Request", firstLine(err.Error()))
}

// responseErrorHandler 处理 handler 返回的错误以及响应写出失败。
func (s *Server) responseErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		writeProblem(w, r, apiErr.Status, apiErr.Code, apiErr.Title, apiErr.Detail)
		return
	}

	// 未预期的错误：记录完整日志，但对调用方只暴露笼统信息。
	s.logger.Error("处理请求失败",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Any("error", err),
	)
	writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal Server Error", "服务内部错误，请稍后重试或以 request id 联系维护者")
}
