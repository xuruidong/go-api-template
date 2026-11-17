// Package store 定义业务领域模型与持久化抽象。
//
// 模板内置的是内存实现（Memory），用于开箱即跑。
// 接入真实数据库时，只需实现这里的 Store 接口（例如 postgres.Store），
// HTTP 层无需改动。
package store

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 表示目标资源不存在。
//
// HTTP 层通过 errors.Is 判断它并映射为 404，而不是把存储层错误直接透出。
var ErrNotFound = errors.New("resource not found")

// Todo 是待办事项的领域模型。
//
// 注意：它和 API 契约里的 Todo schema 是两个东西。
// 领域模型服务于业务逻辑，API 模型（internal/api 中的生成代码）服务于传输格式。
// 两者解耦可以让接口演进（改名、拆分、加字段）不影响存储结构。
type Todo struct {
	ID          int64
	Title       string
	Description *string
	Completed   bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateParams 描述一次创建操作。
type CreateParams struct {
	Title       string
	Description *string
	Completed   bool
}

// Patch 描述一次局部更新。
//
// 指针字段为 nil 表示“本次不更新该字段”，非 nil 表示更新为新值；
// Description 指向空字符串表示清空描述。
type Patch struct {
	Title       *string
	Description *string
	Completed   *bool
}

// ListFilter 描述列表查询条件。
type ListFilter struct {
	Limit     int32
	Offset    int32
	Completed *bool
	Query     string
}

// Store 是待办事项的持久化接口。
//
// 所有方法都必须尊重 ctx 的取消与超时语义。
type Store interface {
	Create(ctx context.Context, params CreateParams) (Todo, error)
	Get(ctx context.Context, id int64) (Todo, error)
	// List 返回当前页数据以及满足过滤条件的总条数。
	List(ctx context.Context, filter ListFilter) (items []Todo, total int64, err error)
	Update(ctx context.Context, id int64, patch Patch) (Todo, error)
	Delete(ctx context.Context, id int64) error
}
