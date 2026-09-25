package pocketfts

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSchemaForFTSTranslatesSearchableToIndexed verifies that the JSON built
// for ftscore uses the key "indexed", even though pocket-fts's own public
// schema uses "searchable". ftscore's own schema parser expects "indexed" —
// this translation is what lets the public field be renamed without
// changing ftscore's behavior.
func TestSchemaForFTSTranslatesSearchableToIndexed(t *testing.T) {
	schema := CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "title", Type: "text", Searchable: true, Weight: 2.0},
			{Name: "body", Type: "text", Searchable: true},
			{Name: "status", Type: "text"},
		},
	}

	out, err := schemaForFTS(schema)
	if err != nil {
		t.Fatalf("schemaForFTS failed: %v", err)
	}

	if strings.Contains(string(out), "searchable") {
		t.Fatalf("ftscore payload must not contain \"searchable\", got: %s", out)
	}

	var parsed struct {
		Fields []struct {
			Name    string `json:"name"`
			Indexed bool   `json:"indexed"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("failed to parse schemaForFTS output: %v", err)
	}

	got := map[string]bool{}
	for _, f := range parsed.Fields {
		got[f.Name] = f.Indexed
	}
	want := map[string]bool{"id": false, "title": true, "body": true, "status": false}
	for name, wantIndexed := range want {
		if got[name] != wantIndexed {
			t.Errorf("field %q: indexed = %v, want %v", name, got[name], wantIndexed)
		}
	}
}

// TestSchemaForFTSDropsFieldPrimaryKeyFlag verifies the per-field
// "primary_key" flag never reaches ftscore, which rejects it; ftscore takes
// the primary key from the schema level only.
func TestSchemaForFTSDropsFieldPrimaryKeyFlag(t *testing.T) {
	out, err := schemaForFTS(CollectionSchema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text", PrimaryKey: true},
			{Name: "title", Type: "text", Searchable: true},
		},
	})
	if err != nil {
		t.Fatalf("schemaForFTS failed: %v", err)
	}

	var parsed struct {
		PrimaryKey string                   `json:"primary_key"`
		Fields     []map[string]interface{} `json:"fields"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if parsed.PrimaryKey != "id" {
		t.Fatalf("schema-level primary_key = %q, want \"id\"", parsed.PrimaryKey)
	}
	for _, field := range parsed.Fields {
		if _, ok := field["primary_key"]; ok {
			t.Fatalf("field %v still carries primary_key: %s", field["name"], out)
		}
	}
}
