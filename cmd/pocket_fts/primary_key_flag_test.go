package main

import (
	"context"
	"net/http"
	"testing"
)

// TestFieldPrimaryKeyFlagOnFTSCollection verifies that the documented
// convenience flag "primary_key": true on the primary key field works for a
// collection with a searchable field. It used to be forwarded to ftscore,
// which rejects it, and the request failed with 500.
func TestFieldPrimaryKeyFlagOnFTSCollection(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "flagged",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text", "primary_key": true},
			{"name": "title", "type": "text", "searchable": true},
		},
	})
	if code != http.StatusCreated {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	code, body = callHandler(t, handleDocumentUpsert, map[string]interface{}{
		"collection": "flagged",
		"document":   map[string]interface{}{"id": "f1", "title": "旗標 標記"},
	})
	if code != http.StatusOK {
		t.Fatalf("upsert returned HTTP %d: %s", code, body)
	}
	if got := queryIDs(t, map[string]interface{}{
		"collection": "flagged", "search": map[string]string{"term": "旗標"}, "limit": 10,
	}); len(got) != 1 || got[0] != "f1" {
		t.Fatalf("search found %v, want [f1]", got)
	}
}

// TestFieldPrimaryKeyFlagOnPlainCollection guards the case that already
// worked: without a searchable field, ftscore is never involved.
func TestFieldPrimaryKeyFlagOnPlainCollection(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "flagged_plain",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text", "primary_key": true},
			{"name": "title", "type": "text"},
		},
	})
	if code != http.StatusCreated {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}
}

// TestFieldPrimaryKeyFlagMismatchIsRejected verifies that flagging a field
// other than the schema's primary key fails with 400 and creates nothing.
func TestFieldPrimaryKeyFlagMismatchIsRejected(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "mismatch",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "title", "type": "text", "searchable": true, "primary_key": true},
		},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("collection create returned HTTP %d, want 400: %s", code, body)
	}

	if _, err := store.Schema(context.Background(), "mismatch"); err == nil {
		t.Fatal("a schema was saved for a rejected collection")
	}
	var tables int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'mismatch'").Scan(&tables); err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if tables != 0 {
		t.Fatal("a table was created for a rejected collection")
	}
	if _, err := store.Search(context.Background(), "mismatch", `{"query":"x","limit":1}`); err == nil {
		t.Fatal("an FTS collection exists for a rejected collection")
	}
}
