package server

import (
	"context"
	"fmt"

	"github.com/example/go-api-template/internal/api"
	"github.com/example/go-api-template/internal/store"
)

// 分页默认值与上限，必须与 api/openapi.yaml 中的 default / minimum / maximum 保持一致。
//
// 校验中间件已经保证传入值在合法范围内，这里只需要处理“未传”的情况。
const (
	defaultLimit  int32 = 20
	defaultOffset int32 = 0
)

// ListTodos 实现 GET /todos。
func (s *Server) ListTodos(ctx context.Context, request api.ListTodosRequestObject) (api.ListTodosResponseObject, error) {
	filter := store.ListFilter{
		Limit:  defaultLimit,
		Offset: defaultOffset,
	}
	params := request.Params
	if params.Limit != nil {
		filter.Limit = int32(*params.Limit)
	}
	if params.Offset != nil {
		filter.Offset = int32(*params.Offset)
	}
	if params.Completed != nil {
		completed := *params.Completed
		filter.Completed = &completed
	}
	if params.Q != nil {
		filter.Query = string(*params.Q)
	}

	todos, total, err := s.store.List(ctx, filter)
	if err != nil {
		return nil, errFromStore(err)
	}

	items := make([]api.Todo, 0, len(todos))
	for _, todo := range todos {
		items = append(items, toAPITodo(todo))
	}

	return api.ListTodos200JSONResponse(api.TodoList{
		Items:  items,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	}), nil
}

// CreateTodo 实现 POST /todos。
func (s *Server) CreateTodo(ctx context.Context, request api.CreateTodoRequestObject) (api.CreateTodoResponseObject, error) {
	if request.Body == nil {
		return nil, errBadRequest("请求体不能为空")
	}

	params := store.CreateParams{
		Title:       request.Body.Title,
		Description: request.Body.Description,
	}
	if request.Body.Completed != nil {
		params.Completed = *request.Body.Completed
	}

	todo, err := s.store.Create(ctx, params)
	if err != nil {
		return nil, errFromStore(err)
	}

	return api.CreateTodo201JSONResponse{
		Body: toAPITodo(todo),
		Headers: api.CreateTodo201ResponseHeaders{
			// 201 响应通过 Location 头告诉客户端新资源地址。
			Location: fmt.Sprintf("/todos/%d", todo.ID),
		},
	}, nil
}

// GetTodo 实现 GET /todos/{id}。
func (s *Server) GetTodo(ctx context.Context, request api.GetTodoRequestObject) (api.GetTodoResponseObject, error) {
	todo, err := s.store.Get(ctx, int64(request.ID))
	if err != nil {
		return nil, errFromStore(err)
	}
	return api.GetTodo200JSONResponse(toAPITodo(todo)), nil
}

// UpdateTodo 实现 PATCH /todos/{id}。
//
// 局部更新语义：请求体中未出现的字段保持不变，出现则覆盖。
func (s *Server) UpdateTodo(ctx context.Context, request api.UpdateTodoRequestObject) (api.UpdateTodoResponseObject, error) {
	if request.Body == nil {
		return nil, errBadRequest("请求体不能为空")
	}

	todo, err := s.store.Update(ctx, int64(request.ID), store.Patch{
		Title:       request.Body.Title,
		Description: request.Body.Description,
		Completed:   request.Body.Completed,
	})
	if err != nil {
		return nil, errFromStore(err)
	}
	return api.UpdateTodo200JSONResponse(toAPITodo(todo)), nil
}

// DeleteTodo 实现 DELETE /todos/{id}。
func (s *Server) DeleteTodo(ctx context.Context, request api.DeleteTodoRequestObject) (api.DeleteTodoResponseObject, error) {
	if err := s.store.Delete(ctx, int64(request.ID)); err != nil {
		return nil, errFromStore(err)
	}
	return api.DeleteTodo204Response{}, nil
}

// toAPITodo 把领域模型转换成 API 模型。
//
// 生成代码使用的是值语义（Todo），没有可选字段指针歧义，转换是显式的，
// 这样领域模型新增内部字段时不会意外暴露到 HTTP 响应里。
func toAPITodo(t store.Todo) api.Todo {
	return api.Todo{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Completed:   t.Completed,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}
