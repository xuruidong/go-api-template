package server_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/example/go-api-template/internal/server"
	"github.com/example/go-api-template/internal/store"
	"github.com/example/go-api-template/pkg/client"
)

const testToken = "integration-test-token"

// newTestServer 启动一个真实监听端口的 HTTP 服务，并返回基于生成客户端构造的调用入口。
//
// 走真实 TCP 而不是直接调用 handler，是为了覆盖：
// 路由注册、参数绑定、请求体解码、中间件顺序、响应状态码与 Content-Type。
func newTestServer(t *testing.T) *client.ClientWithResponses {
	t.Helper()

	st := store.NewMemory()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(st, logger, testToken, "test")

	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	apiClient, err := client.NewClientWithResponses(ts.URL,
		client.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+testToken)
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}
	return apiClient
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestTodoLifecycle(t *testing.T) {
	api := newTestServer(t)
	ctx := context.Background()

	created, err := api.CreateTodoWithResponse(ctx, client.CreateTodoJSONRequestBody{
		Title:       "write docs",
		Description: strPtr("cover the template"),
	})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if created.StatusCode() != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", created.StatusCode(), created.Body)
	}
	if created.JSON201 == nil {
		t.Fatalf("expected JSON201 body, raw=%s", created.Body)
	}
	if created.Headers201 == nil || created.Headers201.Location == "" {
		t.Fatalf("expected Location header, got %+v", created.Headers201)
	}

	id := created.JSON201.ID

	got, err := api.GetTodoWithResponse(ctx, id)
	if err != nil {
		t.Fatalf("GetTodo: %v", err)
	}
	if got.StatusCode() != http.StatusOK || got.JSON200 == nil {
		t.Fatalf("expected 200 with body, got %d: %s", got.StatusCode(), got.Body)
	}
	if got.JSON200.Title != "write docs" {
		t.Fatalf("unexpected title %q", got.JSON200.Title)
	}

	updated, err := api.UpdateTodoWithResponse(ctx, id, client.UpdateTodoJSONRequestBody{
		Completed: boolPtr(true),
	})
	if err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}
	if updated.StatusCode() != http.StatusOK || updated.JSON200 == nil {
		t.Fatalf("expected 200, got %d: %s", updated.StatusCode(), updated.Body)
	}
	if !updated.JSON200.Completed {
		t.Fatal("expected completed=true after patch")
	}
	// 局部更新不应清空未提交的字段。
	if updated.JSON200.Title != "write docs" {
		t.Fatalf("title should be preserved, got %q", updated.JSON200.Title)
	}

	list, err := api.ListTodosWithResponse(ctx, &client.ListTodosParams{})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if list.StatusCode() != http.StatusOK || list.JSON200 == nil {
		t.Fatalf("expected 200, got %d: %s", list.StatusCode(), list.Body)
	}
	if list.JSON200.Total != 1 || len(list.JSON200.Items) != 1 {
		t.Fatalf("expected single item, got %+v", list.JSON200)
	}

	deleted, err := api.DeleteTodoWithResponse(ctx, id)
	if err != nil {
		t.Fatalf("DeleteTodo: %v", err)
	}
	if deleted.StatusCode() != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", deleted.StatusCode(), deleted.Body)
	}

	missing, err := api.GetTodoWithResponse(ctx, id)
	if err != nil {
		t.Fatalf("GetTodo after delete: %v", err)
	}
	if missing.StatusCode() != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", missing.StatusCode())
	}
}

func TestListSupportsFilterAndPagination(t *testing.T) {
	api := newTestServer(t)
	ctx := context.Background()

	for _, title := range []string{"alpha", "beta", "gamma"} {
		if _, err := api.CreateTodoWithResponse(ctx, client.CreateTodoJSONRequestBody{Title: title}); err != nil {
			t.Fatalf("CreateTodo(%s): %v", title, err)
		}
	}

	limit := client.Limit(2)
	page, err := api.ListTodosWithResponse(ctx, &client.ListTodosParams{Limit: &limit})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if page.JSON200 == nil || page.JSON200.Total != 3 || len(page.JSON200.Items) != 2 {
		t.Fatalf("unexpected page: %+v", page.JSON200)
	}

	offset := client.Offset(2)
	last, err := api.ListTodosWithResponse(ctx, &client.ListTodosParams{Limit: &limit, Offset: &offset})
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if last.JSON200 == nil || len(last.JSON200.Items) != 1 {
		t.Fatalf("unexpected last page: %+v", last.JSON200)
	}

	q := client.Query("BET")
	search, err := api.ListTodosWithResponse(ctx, &client.ListTodosParams{Q: &q})
	if err != nil {
		t.Fatalf("ListTodos with q: %v", err)
	}
	if search.JSON200 == nil || search.JSON200.Total != 1 {
		t.Fatalf("expected case-insensitive match, got %+v", search.JSON200)
	}
}

func TestCreateRejectsInvalidBody(t *testing.T) {
	api := newTestServer(t)
	ctx := context.Background()

	// title 为必填且 minLength=1：空标题应被 OpenAPI 校验中间件拒绝，返回 400 而不是 500。
	resp, err := api.CreateTodoWithResponse(ctx, client.CreateTodoJSONRequestBody{Title: ""})
	if err != nil {
		t.Fatalf("CreateTodo: %v", err)
	}
	if resp.StatusCode() != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode(), resp.Body)
	}
	if resp.JSON400 == nil {
		t.Fatalf("expected Problem body, got %s", resp.Body)
	}
	if resp.JSON400.Code == nil || *resp.JSON400.Code != "invalid_request" {
		t.Fatalf("unexpected error code: %+v", resp.JSON400)
	}
}

func TestUpdateRejectsEmptyBody(t *testing.T) {
	api := newTestServer(t)
	ctx := context.Background()

	created, err := api.CreateTodoWithResponse(ctx, client.CreateTodoJSONRequestBody{Title: "t"})
	if err != nil || created.JSON201 == nil {
		t.Fatalf("CreateTodo: %v", err)
	}

	// minProperties: 1 —— 空对象应被拒绝。
	resp, err := api.UpdateTodoWithResponse(ctx, created.JSON201.ID, client.UpdateTodoJSONRequestBody{})
	if err != nil {
		t.Fatalf("UpdateTodo: %v", err)
	}
	if resp.StatusCode() != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode(), resp.Body)
	}
}

func TestUnauthenticatedRequestIsRejected(t *testing.T) {
	st := store.NewMemory()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(st, logger, testToken, "test")
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	anon, err := client.NewClientWithResponses(ts.URL)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}

	ctx := context.Background()

	// 需要鉴权的接口：无 Token -> 401。
	unauthorized, err := anon.ListTodosWithResponse(ctx, nil)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if unauthorized.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", unauthorized.StatusCode(), unauthorized.Body)
	}

	// 错误 Token -> 401。
	wrongToken, err := client.NewClientWithResponses(ts.URL,
		client.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer wrong-token")
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("NewClientWithResponses: %v", err)
	}
	resp, err := wrongToken.ListTodosWithResponse(ctx, nil)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if resp.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode())
	}

	// 公开接口：健康检查无需鉴权。
	health, err := anon.GetHealthWithResponse(ctx)
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if health.StatusCode() != http.StatusOK || health.JSON200 == nil {
		t.Fatalf("expected public health endpoint to succeed, got %d: %s", health.StatusCode(), health.Body)
	}
}

func TestUnknownRouteReturnsNotFound(t *testing.T) {
	st := store.NewMemory()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(st, logger, testToken, "test")
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+"/nope", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("expected json problem body, got %q", ct)
	}
}

func TestOpenAPISpecEndpoint(t *testing.T) {
	st := store.NewMemory()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(st, logger, testToken, "test")
	handler, err := srv.Handler()
	if err != nil {
		t.Fatalf("Handler(): %v", err)
	}
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	resp, err := http.Get(ts.URL + "/openapi.json")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected non-empty spec")
	}
}

func TestPrincipalFromContext(t *testing.T) {
	ctx := server.WithPrincipal(context.Background(), server.Principal{Subject: "user-1"})
	p, ok := server.PrincipalFromContext(ctx)
	if !ok || p.Subject != "user-1" {
		t.Fatalf("unexpected principal: %+v ok=%v", p, ok)
	}

	if _, ok := server.PrincipalFromContext(context.Background()); ok {
		t.Fatal("expected no principal in empty context")
	}
}
