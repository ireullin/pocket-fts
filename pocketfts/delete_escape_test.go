package pocketfts

import (
	"context"
	"testing"
	"time"
)

// TestDeleteHandlesQuotesInID verifies the primary key sent to ftscore on
// delete is JSON-encoded, so an id with a quote or backslash removes that
// document from the full-text index instead of producing broken JSON.
func TestDeleteHandlesQuotesInID(t *testing.T) {
	s, dbPath := openLockTestStore(t, 5*time.Second)
	ctx := context.Background()
	id := `a"b\c`

	if err := s.Upsert(ctx, "docs", Document{"id": id, "body": "引號標記"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := s.Delete(ctx, "docs", id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if hits := searchHits(t, s, "引號標記"); len(hits) != 0 {
		t.Fatalf("deleted document is still in the index: %v", hits)
	}
	if _, ok := storedBody(t, dbPath, id); ok {
		t.Fatal("deleted document is still in the table")
	}
}

// TestCreatingAnExistingFTSCollectionFails pins the ftscore behavior that
// TestFieldPrimaryKeyFlagMismatchIsRejected relies on.
func TestCreatingAnExistingFTSCollectionFails(t *testing.T) {
	s, _ := openLockTestStore(t, 5*time.Second)
	if err := s.fts.CreateCollection(`{"name":"docs","primary_key":"id","fields":[{"name":"id","type":"text"},{"name":"body","type":"text","indexed":true}]}`); err == nil {
		t.Fatal("ftscore accepted a second create of an existing collection")
	}
}
