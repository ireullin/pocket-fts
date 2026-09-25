package pocketfts

import "testing"

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
