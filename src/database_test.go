package main

import (
	"path/filepath"
	"testing"
	"time"
)

// TestInitDBWaitsOnBusyLock verifies that concurrent writers on the *sql.DB
// returned by initDB wait for a busy_timeout retry instead of failing
// immediately with "database is locked". Before the fix, initDB opened the
// database with no busy_timeout at all, so any lock contention between the
// Go connection pool and the ftscore C engine (which share the same
// db.sqlite file) surfaced as an instant SQLITE_BUSY error.
func TestInitDBWaitsOnBusyLock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	testDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer testDB.Close()

	tx, err := testDB.Begin()
	if err != nil {
		t.Fatalf("failed to begin holder tx: %v", err)
	}
	if _, err := tx.Exec("INSERT INTO collections (name, schema_json) VALUES ('holder', '{}')"); err != nil {
		t.Fatalf("failed to write inside holder tx: %v", err)
	}

	held := make(chan struct{})
	go func() {
		close(held)
		time.Sleep(300 * time.Millisecond)
		if commitErr := tx.Commit(); commitErr != nil {
			t.Errorf("failed to commit holder tx: %v", commitErr)
		}
	}()
	<-held

	start := time.Now()
	_, err = testDB.Exec("INSERT INTO collections (name, schema_json) VALUES ('waiter', '{}')")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected concurrent write to wait for the busy lock and succeed, got error after %v: %v", elapsed, err)
	}
	if elapsed < 250*time.Millisecond {
		t.Fatalf("concurrent write succeeded too quickly (%v); busy_timeout does not appear to be applied to this connection", elapsed)
	}
}
