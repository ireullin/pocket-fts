package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestInitDBUsesWAL verifies that initDB opens db.sqlite in WAL mode.
// WAL mode is what lets fsync happen only at checkpoint time (instead of
// every commit) and lets readers proceed without waiting for an in-progress
// writer's commit.
func TestInitDBUsesWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	testDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer testDB.Close()

	var mode string
	if err := testDB.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("failed to read journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want %q", mode, "wal")
	}
}

// TestWALFallbackIsRejected verifies that verifyJournalModeWAL reports an
// error when SQLite silently falls back to a non-WAL mode instead of
// honoring the request. ":memory:" databases always report "memory"
// regardless of what journal_mode is requested, which makes them a
// deterministic way to trigger the fallback without relying on filesystem
// tricks.
func TestWALFallbackIsRejected(t *testing.T) {
	memDB, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatalf("failed to open in-memory database: %v", err)
	}
	defer memDB.Close()

	if err := verifyJournalModeWAL(memDB); err == nil {
		t.Fatalf("expected an error when journal_mode falls back to non-WAL, got nil")
	}
}

// TestCheckpointTruncatesWAL verifies that checkpointWAL writes the WAL
// file's contents back into the main database file and truncates it,
// leaving a clean WAL file behind (the behavior wired to the SIGTERM
// handler for a graceful shutdown).
func TestCheckpointTruncatesWAL(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	testDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer testDB.Close()

	for i := 0; i < 500; i++ {
		if _, err := testDB.Exec(
			"INSERT INTO collections (name, schema_json) VALUES (?, ?)",
			fmt.Sprintf("c%04d", i), "{}"); err != nil {
			t.Fatalf("seed insert %d failed: %v", i, err)
		}
	}

	walPath := dbPath + "-wal"
	before, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("expected a WAL file to exist before checkpoint: %v", err)
	}
	if before.Size() == 0 {
		t.Fatalf("expected the WAL file to have content before checkpoint")
	}

	if err := checkpointWAL(testDB); err != nil {
		t.Fatalf("checkpointWAL failed: %v", err)
	}

	after, err := os.Stat(walPath)
	if err != nil {
		t.Fatalf("expected the WAL file to still exist after checkpoint: %v", err)
	}
	if after.Size() != 0 {
		t.Fatalf("expected the WAL file to be truncated to 0 bytes after checkpoint, got %d bytes", after.Size())
	}
}
