package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/getkin/kin-openapi/openapi3filter"
)

// bearerSchemeName 必须与 api/openapi.yaml 中 components.securitySchemes 的键一致。
const bearerSchemeName = "bearerAuth"

// Principal 表示已认证的调用方身份。
//
// 模板内置的是静态 Token 鉴权，因此 Subject 固定。
// 接入真实鉴权（JWT / OIDC）时，把这里换成解析出的用户信息即可，
// handler 层通过 PrincipalFromContext 读取。
type Principal struct {
	Subject string
	Scopes  []string
}

type principalCtxKey struct{}

// WithPrincipal 将身份写入 context。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// PrincipalFromContext 读取身份；未认证时 ok 为 false。
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(Principal)
	return p, ok
}

// authenticate 是交给请求校验中间件的 AuthenticationFunc。
//
// 是否要求认证、需要哪些 scope，完全由 OpenAPI 文档中的 security 定义驱动：
// 公开接口在文档里写 security: []，这里不需要任何特判。
func (s *Server) authenticate(_ context.Context, input *openapi3filter.AuthenticationInput) error {
	if input.SecuritySchemeName != bearerSchemeName {
		return input.NewError(nil)
	}

	token, ok := bearerToken(input.RequestValidationInput.Request)
	if !ok {
		return input.NewError(errors.New("missing Authorization: Bearer <token> header"))
	}
	if !s.tokenMatches(token) {
		return input.NewError(errors.New("invalid bearer token"))
	}
	return nil
}

// principalMiddleware 在校验中间件确认身份合法之后，把身份放进 context 供 handler 使用。
//
// 职责拆分：校验中间件负责“是否放行”，这里负责“把谁放进请求上下文”。
// 未携带 Token 或 Token 非法的请求在到达这里之前已经被拒绝。
func (s *Server) principalMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok || !s.tokenMatches(token) {
			next.ServeHTTP(w, r)
			return
		}
		ctx := WithPrincipal(r.Context(), Principal{Subject: "demo-user"})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) tokenMatches(token string) bool {
	// 定长比较，避免通过响应时间推测 Token 前缀。
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1
}

// bearerToken 从请求头解析 Bearer Token（大小写不敏感）。
func bearerToken(r *http.Request) (string, bool) {
	const prefix = "bearer "
	header := r.Header.Get("Authorization")
	if len(header) <= len(prefix) || !strings.EqualFold(header[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(prefix):])
	if token == "" {
		return "", false
	}
	return token, true
}
