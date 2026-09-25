package pocketfts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// openLockTestStore opens a store with one searchable collection "docs".
func openLockTestStore(t *testing.T, writeTimeout time.Duration) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	s, err := Open(Config{Path: dbPath, WriteTimeout: writeTimeout})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	err = s.CreateCollection(context.Background(), Schema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "body", Type: "text", Searchable: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	return s, dbPath
}

// searchHits returns the ids the full-text index finds for term.
func searchHits(t *testing.T, s *Store, term string) []string {
	t.Helper()
	rows, err := s.Query(context.Background(), "docs",
		Node{Search: &SearchQuery{Term: term}}, Result{Fields: []string{"id"}})
	if err != nil {
		t.Fatalf("Query(%q) failed: %v", term, err)
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row["id"].(string))
	}
	return ids
}

// storedBody reads the body column straight from the SQL table.
func storedBody(t *testing.T, dbPath, id string) (string, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var body string
	err = db.QueryRow("SELECT body FROM docs WHERE id = ?", id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false
	}
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body, true
}

// TestConcurrentUpsertsKeepIndexAndTableInSync reproduces two writers on the
// same document interleaving between their two writes: A writes one store,
// B writes both, then A writes the other. Without a lock around the pair,
// the full-text index ends up with B's text while the table holds A's.
func TestConcurrentUpsertsKeepIndexAndTableInSync(t *testing.T) {
	s, dbPath := openLockTestStore(t, 5*time.Second)
	ctx := context.Background()

	bDone := make(chan struct{})
	var calls atomic.Int32
	s.betweenWrites = func() {
		if calls.Add(1) != 1 {
			return
		}
		// Writer A is between its two writes. Give writer B the chance to
		// run to completion here; with the lock in place B cannot start, so
		// A moves on after a short wait.
		select {
		case <-bDone:
		case <-time.After(300 * time.Millisecond):
		}
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.Upsert(ctx, "docs", Document{"id": "a", "body": "甲標記"}); err != nil {
			t.Errorf("upsert A: %v", err)
		}
	}()
	for calls.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	if err := s.Upsert(ctx, "docs", Document{"id": "a", "body": "乙標記"}); err != nil {
		t.Fatalf("upsert B: %v", err)
	}
	close(bDone)
	wg.Wait()

	body, _ := storedBody(t, dbPath, "a")
	hitsA := searchHits(t, s, "甲標記")
	hitsB := searchHits(t, s, "乙標記")

	var indexed string
	switch {
	case len(hitsA) == 1 && len(hitsB) == 0:
		indexed = "甲標記"
	case len(hitsA) == 0 && len(hitsB) == 1:
		indexed = "乙標記"
	default:
		t.Fatalf("index holds both or neither: 甲 hits %v, 乙 hits %v", hitsA, hitsB)
	}
	if indexed != body {
		t.Fatalf("index and table disagree: the table holds %q but the index finds %q", body, indexed)
	}
}

// TestFailedFTSWriteLeavesTableUnchanged verifies that when ftscore rejects a
// document, the SQL table keeps its previous row.
func TestFailedFTSWriteLeavesTableUnchanged(t *testing.T) {
	s, dbPath := openLockTestStore(t, 5*time.Second)
	ctx := context.Background()

	if err := s.Upsert(ctx, "docs", Document{"id": "a", "body": "舊內容"}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	// ftscore requires a string for a searchable text field; SQLite would
	// store the number without complaint.
	if err := s.Upsert(ctx, "docs", Document{"id": "a", "body": 123}); err == nil {
		t.Fatal("expected ftscore to reject a number in a searchable text field")
	}
	if body, _ := storedBody(t, dbPath, "a"); body != "舊內容" {
		t.Fatalf("table body = %q after a failed write, want %q", body, "舊內容")
	}

	if err := s.Upsert(ctx, "docs", Document{"id": "b", "body": 456}); err == nil {
		t.Fatal("expected ftscore to reject a number in a searchable text field")
	}
	if _, ok := storedBody(t, dbPath, "b"); ok {
		t.Fatal("a document ftscore rejected was still inserted into the table")
	}
}

// TestWriteWaitingForLockTimesOut verifies that waiting for the write lock
// counts against the write timeout and ends in ErrWriteTimeout.
func TestWriteWaitingForLockTimesOut(t *testing.T) {
	s, _ := openLockTestStore(t, 200*time.Millisecond)

	release, err := s.acquireWrite(context.Background())
	if err != nil {
		t.Fatalf("acquireWrite: %v", err)
	}
	defer release()

	start := time.Now()
	err = s.Upsert(context.Background(), "docs", Document{"id": "a", "body": "排隊"})
	elapsed := time.Since(start)

	if !IsTimeout(err) {
		t.Fatalf("got %v, want an error wrapping ErrWriteTimeout", err)
	}
	if elapsed < 150*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("gave up after %v; want about the 200ms write timeout", elapsed)
	}

	err = s.Delete(context.Background(), "docs", "a")
	if !IsTimeout(err) {
		t.Fatalf("delete: got %v, want an error wrapping ErrWriteTimeout", err)
	}
}
