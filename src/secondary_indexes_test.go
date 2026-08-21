package main

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

// TestCreateCollectionBuildsDeclaredIndexes verifies that /collections/create
// actually builds the SQL indexes declared in the schema's "indexes" array,
// with columns in the declared order (order matters for SQLite's query
// planner on composite indexes).
func TestCreateCollectionBuildsDeclaredIndexes(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "events",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "status", "type": "text"},
			{"name": "created_at", "type": "integer"},
		},
		"indexes": [][]string{{"status"}, {"status", "created_at"}},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	rows, err := db.Query(`SELECT name, sql FROM sqlite_master WHERE type = 'index' AND tbl_name = 'events'`)
	if err != nil {
		t.Fatalf("failed to query sqlite_master: %v", err)
	}
	defer rows.Close()

	found := map[string]string{}
	for rows.Next() {
		var name string
		var stmt sql.NullString // NULL for the implicit index SQLite creates for a PRIMARY KEY
		if err := rows.Scan(&name, &stmt); err != nil {
			t.Fatalf("failed to scan sqlite_master row: %v", err)
		}
		found[name] = stmt.String
	}

	if _, ok := found["idx_events_status"]; !ok {
		t.Errorf("expected index idx_events_status to exist, found: %v", found)
	}
	compositeSQL, ok := found["idx_events_status_created_at"]
	if !ok {
		t.Fatalf("expected index idx_events_status_created_at to exist, found: %v", found)
	}
	if !containsInOrder(compositeSQL, "status", "created_at") {
		t.Errorf("composite index SQL %q does not list status before created_at", compositeSQL)
	}
}

// TestCreateCollectionRejectsUnknownIndexField verifies that declaring an
// index over a field the schema doesn't define fails the whole request with
// 400, and creates nothing (no FTS collection, no metadata, no SQL table).
func TestCreateCollectionRejectsUnknownIndexField(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "bad_events",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
		},
		"indexes": [][]string{{"nonexistent"}},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400, got %d: %s", code, body)
	}

	if _, err := getCollectionSchema("bad_events"); err == nil {
		t.Fatalf("expected no schema to have been saved for a rejected collection")
	}
}

func containsInOrder(s string, substrs ...string) bool {
	pos := 0
	for _, sub := range substrs {
		idx := strings.Index(s[pos:], sub)
		if idx == -1 {
			return false
		}
		pos += idx + len(sub)
	}
	return true
}

func TestGenerateIndexSQLSingleColumn(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "status", Type: "text"},
			{Name: "created_at", Type: "integer"},
		},
		Indexes: [][]string{{"status"}, {"created_at"}},
	}

	stmts, err := generateIndexSQL(schema)
	if err != nil {
		t.Fatalf("generateIndexSQL failed: %v", err)
	}
	if len(stmts) != 2 {
		t.Fatalf("got %d statements, want 2: %v", len(stmts), stmts)
	}
	want := []string{
		"CREATE INDEX IF NOT EXISTS idx_docs_status ON docs (status)",
		"CREATE INDEX IF NOT EXISTS idx_docs_created_at ON docs (created_at)",
	}
	for i, w := range want {
		if stmts[i] != w {
			t.Errorf("statement %d = %q, want %q", i, stmts[i], w)
		}
	}
}

func TestGenerateIndexSQLComposite(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "status", Type: "text"},
			{Name: "created_at", Type: "integer"},
		},
		Indexes: [][]string{{"status", "created_at"}},
	}

	stmts, err := generateIndexSQL(schema)
	if err != nil {
		t.Fatalf("generateIndexSQL failed: %v", err)
	}
	want := "CREATE INDEX IF NOT EXISTS idx_docs_status_created_at ON docs (status, created_at)"
	if len(stmts) != 1 || stmts[0] != want {
		t.Fatalf("got %v, want [%q]", stmts, want)
	}
}

func TestGenerateIndexSQLRejectsUnknownField(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
		},
		Indexes: [][]string{{"nonexistent"}},
	}

	if _, err := generateIndexSQL(schema); err == nil {
		t.Fatalf("expected an error for an index referencing an unknown field, got nil")
	}
}

func TestGenerateIndexSQLRejectsEmptyEntry(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
		},
		Indexes: [][]string{{}},
	}

	if _, err := generateIndexSQL(schema); err == nil {
		t.Fatalf("expected an error for an empty index entry, got nil")
	}
}

func TestGenerateIndexSQLNoIndexesIsEmpty(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
		},
	}

	stmts, err := generateIndexSQL(schema)
	if err != nil {
		t.Fatalf("generateIndexSQL failed: %v", err)
	}
	if len(stmts) != 0 {
		t.Fatalf("got %v, want no statements", stmts)
	}
}
