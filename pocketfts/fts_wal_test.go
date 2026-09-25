package pocketfts

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

// indicesJournalMode 讀取 ftscore 索引資料庫的 journal 模式。必須在引擎關閉
// 之後才呼叫：預設版面下 ftscore 用 locking_mode=EXCLUSIVE 持有檔案，第二條
// 連線開不起來。
func indicesJournalMode(t *testing.T, dbPath string) string {
	t.Helper()
	indices := strings.TrimSuffix(dbPath, filepath.Ext(dbPath)) + ".indices"

	db, err := sql.Open("sqlite", indices)
	if err != nil {
		t.Fatalf("open %s: %v", indices, err)
	}
	defer db.Close()

	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("read journal_mode from %s: %v", indices, err)
	}
	return mode
}

func openEngine(t *testing.T, opts ftsOptions) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	if err := loadFTSLibrary(dbPath); err != nil {
		t.Fatalf("failed to load embedded ftscore library: %v", err)
	}

	engine, err := newFTSEngine(dbPath, 5000, true, opts)
	if err != nil {
		t.Fatalf("newFTSEngine(%+v) failed: %v", opts, err)
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	return dbPath
}

func TestFTSDefaultsToDeleteJournal(t *testing.T) {
	dbPath := openEngine(t, ftsOptions{})
	if got := indicesJournalMode(t, dbPath); got != "delete" {
		t.Fatalf(".indices journal_mode is %q, want \"delete\"", got)
	}
}

func TestFTSWALOptionEnablesWAL(t *testing.T) {
	dbPath := openEngine(t, ftsOptions{WAL: true})
	if got := indicesJournalMode(t, dbPath); got != "wal" {
		t.Fatalf(".indices journal_mode is %q, want \"wal\"", got)
	}
}

func TestFTSWALLeavesNoSidecarAfterClose(t *testing.T) {
	dbPath := openEngine(t, ftsOptions{WAL: true})

	dir := filepath.Dir(dbPath)
	matches, err := filepath.Glob(filepath.Join(dir, "*.indices-*"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no sidecar files after close, found %v", matches)
	}
}
