package store

import (
	"context"
	"errors"
	"testing"
)

func TestMemoryCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	created, err := m.Create(ctx, CreateParams{Title: "first"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID != 1 {
		t.Fatalf("expected ID 1, got %d", created.ID)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Fatal("expected timestamps to be set")
	}

	got, err := m.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "first" {
		t.Fatalf("unexpected title %q", got.Title)
	}

	title := "renamed"
	completed := true
	updated, err := m.Update(ctx, created.ID, Patch{Title: &title, Completed: &completed})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Title != "renamed" || !updated.Completed {
		t.Fatalf("update not applied: %+v", updated)
	}

	if err := m.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := m.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestMemoryUpdateDescriptionCanBeCleared(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	desc := "detail"
	todo, err := m.Create(ctx, CreateParams{Title: "t", Description: &desc})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	empty := ""
	updated, err := m.Update(ctx, todo.ID, Patch{Description: &empty})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Description == nil || *updated.Description != "" {
		t.Fatalf("expected empty description, got %+v", updated.Description)
	}
}

func TestMemoryListFilterAndPaginate(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	for _, title := range []string{"buy milk", "Buy bread", "write docs"} {
		if _, err := m.Create(ctx, CreateParams{Title: title}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	items, total, err := m.List(ctx, ListFilter{Limit: 10, Offset: 0, Query: "buy"})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 matched items, got total=%d len=%d", total, len(items))
	}

	items, total, err = m.List(ctx, ListFilter{Limit: 2, Offset: 0})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(items) != 2 {
		t.Fatalf("expected total=3 page=2, got total=%d len=%d", total, len(items))
	}

	items, total, err = m.List(ctx, ListFilter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 || len(items) != 1 {
		t.Fatalf("expected total=3 page=1, got total=%d len=%d", total, len(items))
	}

	// 越界分页返回空页而不是 panic。
	items, _, err = m.List(ctx, ListFilter{Limit: 10, Offset: 100})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty page, got %d items", len(items))
	}
}

func TestMemoryListFiltersByCompleted(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	done := true
	if _, err := m.Create(ctx, CreateParams{Title: "done", Completed: true}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := m.Create(ctx, CreateParams{Title: "pending"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	items, total, err := m.List(ctx, ListFilter{Limit: 10, Completed: &done})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(items) != 1 || !items[0].Completed {
		t.Fatalf("expected only completed item, got total=%d items=%+v", total, items)
	}
}

func TestMemoryRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := NewMemory()
	if _, err := m.Create(ctx, CreateParams{Title: "t"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
