package cache_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"desktriage.davea.me/cache"
	"desktriage.davea.me/db"
)

func newTestStore(t *testing.T) *cache.Store {
	t.Helper()
	wdb, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { wdb.Close() })
	if err := db.RunMigrations(wdb); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	return cache.NewStore(wdb)
}

func TestGetMiss(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	entry, err := store.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil entry for cache miss, got %+v", entry)
	}
}

func TestSetAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	key := "test-key"
	data := []byte(`{"hello":"world"}`)
	etag := "abc123"
	maxAge := 1 * time.Hour

	if err := store.Set(ctx, key, data, etag, maxAge); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if string(entry.Data) != string(data) {
		t.Errorf("Data = %q, want %q", entry.Data, data)
	}
	if entry.ETag != etag {
		t.Errorf("ETag = %q, want %q", entry.ETag, etag)
	}
	if !entry.Fresh {
		t.Error("expected Fresh=true")
	}
	if entry.CachedAt.IsZero() {
		t.Error("CachedAt should not be zero")
	}
}

func TestStaleEntry(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// maxAge of 1ms truncates to 0 seconds in storage, so entry is stale immediately.
	if err := store.Set(ctx, "stale-key", []byte("data"), "", 1*time.Millisecond); err != nil {
		t.Fatalf("Set: %v", err)
	}

	entry, err := store.Get(ctx, "stale-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if entry.Fresh {
		t.Error("expected Fresh=false for stale entry")
	}
	// Data should still be accessible even when stale.
	if string(entry.Data) != "data" {
		t.Errorf("Data = %q, want %q", entry.Data, "data")
	}
}

func TestDelete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if err := store.Set(ctx, "del-key", []byte("value"), "", time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Delete(ctx, "del-key"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	entry, err := store.Get(ctx, "del-key")
	if err != nil {
		t.Fatalf("Get after delete: %v", err)
	}
	if entry != nil {
		t.Fatalf("expected nil after delete, got %+v", entry)
	}
}

func TestFlush(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	keys := []string{"flush-a", "flush-b", "flush-c"}
	for _, k := range keys {
		if err := store.Set(ctx, k, []byte("val-"+k), "", time.Hour); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}

	if err := store.Flush(ctx); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	for _, k := range keys {
		entry, err := store.Get(ctx, k)
		if err != nil {
			t.Fatalf("Get %s after flush: %v", k, err)
		}
		if entry != nil {
			t.Errorf("expected nil for %s after flush, got %+v", k, entry)
		}
	}
}

func TestSetJSONAndGetJSON(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	type Widget struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}

	src := Widget{Name: "sprocket", Count: 42}
	if err := store.SetJSON(ctx, "widget", src, time.Hour); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}

	var dst Widget
	ok, err := store.GetJSON(ctx, "widget", &dst)
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if !ok {
		t.Fatal("expected GetJSON to return true")
	}
	if dst != src {
		t.Errorf("GetJSON result = %+v, want %+v", dst, src)
	}
}

func TestGetJSONStale(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	type Item struct {
		ID int `json:"id"`
	}

	// 1ms maxAge truncates to 0 seconds → stale immediately.
	if err := store.SetJSON(ctx, "item", Item{ID: 7}, 1*time.Millisecond); err != nil {
		t.Fatalf("SetJSON: %v", err)
	}

	var dst Item
	ok, err := store.GetJSON(ctx, "item", &dst)
	if err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if ok {
		t.Error("expected GetJSON to return false for stale entry")
	}
}

func TestOverwrite(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	key := "overwrite-key"

	if err := store.Set(ctx, key, []byte("first"), "etag-1", time.Hour); err != nil {
		t.Fatalf("Set first: %v", err)
	}
	if err := store.Set(ctx, key, []byte("second"), "etag-2", time.Hour); err != nil {
		t.Fatalf("Set second: %v", err)
	}

	entry, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if entry == nil {
		t.Fatal("expected entry, got nil")
	}
	if string(entry.Data) != "second" {
		t.Errorf("Data = %q, want %q", entry.Data, "second")
	}
	if entry.ETag != "etag-2" {
		t.Errorf("ETag = %q, want %q", entry.ETag, "etag-2")
	}
}
