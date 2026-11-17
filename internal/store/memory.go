package store

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
)

// Memory 是进程内的线程安全 Store 实现。
//
// 数据保存在内存中，进程重启即丢失；仅用于开发、示例与测试。
// 由于不依赖外部服务，它也让集成测试可以在没有数据库的情况下跑通完整链路。
type Memory struct {
	mu     sync.RWMutex
	todos  []Todo // 始终按 ID 升序
	nextID int64
}

// 编译期断言：Memory 必须满足 Store。
var _ Store = (*Memory)(nil)

// NewMemory 创建空的内存存储。
func NewMemory() *Memory {
	return &Memory{nextID: 1}
}

// Create 分配 ID 并写入。
func (m *Memory) Create(ctx context.Context, params CreateParams) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}

	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()

	todo := Todo{
		ID:          m.nextID,
		Title:       params.Title,
		Description: cloneString(params.Description),
		Completed:   params.Completed,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.nextID++
	m.todos = append(m.todos, todo)
	return cloneTodo(todo), nil
}

// Get 按 ID 查询单个待办。
func (m *Memory) Get(ctx context.Context, id int64) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	idx := m.indexOf(id)
	if idx < 0 {
		return Todo{}, ErrNotFound
	}
	return cloneTodo(m.todos[idx]), nil
}

// List 按过滤条件筛选、排序、分页。
func (m *Memory) List(ctx context.Context, filter ListFilter) ([]Todo, int64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	matched := make([]Todo, 0, len(m.todos))
	query := strings.ToLower(filter.Query)
	for _, todo := range m.todos {
		if filter.Completed != nil && todo.Completed != *filter.Completed {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(todo.Title), query) {
			continue
		}
		matched = append(matched, cloneTodo(todo))
	}

	total := int64(len(matched))
	items := slicePage(matched, filter.Offset, filter.Limit)
	return items, total, nil
}

// Update 应用局部更新。
func (m *Memory) Update(ctx context.Context, id int64, patch Patch) (Todo, error) {
	if err := ctx.Err(); err != nil {
		return Todo{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.indexOf(id)
	if idx < 0 {
		return Todo{}, ErrNotFound
	}

	todo := &m.todos[idx]
	if patch.Title != nil {
		todo.Title = *patch.Title
	}
	if patch.Description != nil {
		// 传空字符串表示清空描述。
		todo.Description = cloneString(patch.Description)
	}
	if patch.Completed != nil {
		todo.Completed = *patch.Completed
	}
	todo.UpdatedAt = time.Now().UTC()

	return cloneTodo(*todo), nil
}

// Delete 删除指定待办。
func (m *Memory) Delete(ctx context.Context, id int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	idx := m.indexOf(id)
	if idx < 0 {
		return ErrNotFound
	}
	m.todos = slices.Delete(m.todos, idx, idx+1)
	return nil
}

// indexOf 返回 ID 对应的下标，未找到返回 -1。
// 调用方必须已持有锁。
func (m *Memory) indexOf(id int64) int {
	return slices.IndexFunc(m.todos, func(t Todo) bool { return t.ID == id })
}

// slicePage 对切片做安全分页：越界不 panic，limit 为 0 时返回空页。
func slicePage[T any](items []T, offset, limit int32) []T {
	if limit <= 0 || int(offset) >= len(items) {
		return []T{}
	}
	end := int(offset + limit)
	if end > len(items) || end < 0 {
		end = len(items)
	}
	return items[offset:end]
}

func cloneTodo(t Todo) Todo {
	return Todo{
		ID:          t.ID,
		Title:       t.Title,
		Description: cloneString(t.Description),
		Completed:   t.Completed,
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
	}
}

func cloneString(s *string) *string {
	if s == nil {
		return nil
	}
	v := *s
	return &v
}
