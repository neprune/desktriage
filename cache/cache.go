// Package cache provides a database-backed HTTP response cache for Freshdesk API calls.
package cache

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"desktriage.davea.me/db/dbgen"
)

// Store wraps the sqlc queries to provide a typed cache interface.
type Store struct {
	q *dbgen.Queries
}

// NewStore creates a new cache store.
func NewStore(db *sql.DB) *Store {
	return &Store{q: dbgen.New(db)}
}

// Entry represents a cached response.
type Entry struct {
	Data     []byte
	ETag     string
	CachedAt time.Time
	Fresh    bool // true if within max_age
}

// Get retrieves a cache entry. Returns nil if not found.
// The Fresh field indicates whether the entry is within its max age.
func (s *Store) Get(ctx context.Context, key string) (*Entry, error) {
	row, err := s.q.GetCache(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cache get %q: %w", key, err)
	}

	cachedAt, err := time.Parse(time.RFC3339, row.CachedAt)
	if err != nil {
		// Corrupt cache entry, treat as miss
		slog.Warn("cache: corrupt cached_at", "key", key, "value", row.CachedAt)
		return nil, nil
	}

	etag := ""
	if row.Etag != nil {
		etag = *row.Etag
	}

	age := time.Since(cachedAt)
	fresh := age < time.Duration(row.MaxAgeSecs)*time.Second

	return &Entry{
		Data:     []byte(row.ResponseBody),
		ETag:     etag,
		CachedAt: cachedAt,
		Fresh:    fresh,
	}, nil
}

// Set stores a response in the cache.
func (s *Store) Set(ctx context.Context, key string, data []byte, etag string, maxAge time.Duration) error {
	var etagPtr *string
	if etag != "" {
		etagPtr = &etag
	}
	return s.q.SetCache(ctx, dbgen.SetCacheParams{
		CacheKey:     key,
		ResponseBody: string(data),
		Etag:         etagPtr,
		CachedAt:     time.Now().UTC().Format(time.RFC3339),
		MaxAgeSecs:   int64(maxAge.Seconds()),
	})
}

// Delete removes a specific cache entry.
func (s *Store) Delete(ctx context.Context, key string) error {
	return s.q.DeleteCache(ctx, key)
}

// Flush removes all cache entries.
func (s *Store) Flush(ctx context.Context) error {
	return s.q.DeleteAllCache(ctx)
}

// Cleanup removes expired entries.
func (s *Store) Cleanup(ctx context.Context) error {
	return s.q.DeleteExpiredCache(ctx)
}

// GetJSON retrieves a cached entry and unmarshals it into dst.
// Returns false if cache miss or stale.
func (s *Store) GetJSON(ctx context.Context, key string, dst any) (bool, error) {
	entry, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if entry == nil || !entry.Fresh {
		return false, nil
	}
	if err := json.Unmarshal(entry.Data, dst); err != nil {
		slog.Warn("cache: corrupt JSON", "key", key, "error", err)
		return false, nil
	}
	return true, nil
}

// GetStaleJSON retrieves a cached entry even if expired, and unmarshals it.
// Returns false only if the key doesn't exist at all.
func (s *Store) GetStaleJSON(ctx context.Context, key string, dst any) (bool, error) {
	entry, err := s.Get(ctx, key)
	if err != nil {
		return false, err
	}
	if entry == nil {
		return false, nil
	}
	if err := json.Unmarshal(entry.Data, dst); err != nil {
		slog.Warn("cache: corrupt JSON", "key", key, "error", err)
		return false, nil
	}
	return true, nil
}

// SetJSON marshals v and stores it in the cache.
func (s *Store) SetJSON(ctx context.Context, key string, v any, maxAge time.Duration) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("cache set JSON %q: %w", key, err)
	}
	return s.Set(ctx, key, data, "", maxAge)
}
