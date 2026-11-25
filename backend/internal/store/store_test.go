package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snirye/youtube-guardian/backend/internal/database"
)

func TestRequestDeduplicationAndRejection(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	id := "UC" + strings.Repeat("a", 22)
	first, created, err := s.RequestApproval(ctx, id, "Example", "exampleabcd")
	if err != nil || !created {
		t.Fatalf("create request: created = %v, err = %v", created, err)
	}
	repeated, created, err := s.RequestApproval(ctx, id, "Example", "differentab")
	if err != nil || created || first.ID != repeated.ID {
		t.Fatalf("repeat request: created = %v, err = %v", created, err)
	}
	initial, err := s.Snapshot(ctx)
	if err != nil || len(initial.Channels) != 0 || len(initial.Pending) != 1 || initial.Pending[0] != id || !initial.BlockShorts {
		t.Fatalf("initial snapshot = %+v, err = %v", initial, err)
	}
	if err := s.Resolve(ctx, first.ID, false); err != nil {
		t.Fatal(err)
	}
	if err := s.Resolve(ctx, first.ID, true); !errors.Is(err, ErrNotPending) {
		t.Fatalf("resolve stale button: %v", err)
	}
	if _, _, err := s.RequestApproval(ctx, id, "Example", "exampleabcd"); !errors.Is(err, ErrDecided) {
		t.Fatalf("request blocked channel: %v", err)
	}
	updated, err := s.Snapshot(ctx)
	if err != nil || updated.Version != initial.Version+1 || len(updated.Pending) != 0 || len(updated.Channels) != 1 ||
		updated.Channels[0].Decision != "blocked" {
		t.Fatalf("updated snapshot = %+v, err = %v", updated, err)
	}
}

func TestRetentionPreservesRecentActivity(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	if _, err := s.DB.ExecContext(ctx, "INSERT INTO activity_events (occurred_at_ms, event_type) VALUES (?, 'playback')",
		time.Now().AddDate(0, 0, -31).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := s.Record(ctx, Playback{VideoID: "exampleabcd", Decision: "denied"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Prune(ctx, time.Now().AddDate(0, 0, -30)); err != nil {
		t.Fatal(err)
	}
	events, err := s.Recent(ctx, 10)
	if err != nil || len(events) != 1 || events[0].VideoID != "exampleabcd" {
		t.Fatalf("events after retention = %+v, err = %v", events, err)
	}
}

func testStore(t *testing.T) Store {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "guardian.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Store{DB: db}
}
