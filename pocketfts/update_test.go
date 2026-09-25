package pocketfts

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
)

// openUpdateTestStore opens a store with a "meetings" collection seeded with
// one document, m1.
func openUpdateTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	s, err := Open(Config{Path: dbPath})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	err = s.CreateCollection(ctx, Schema{
		Name:       "meetings",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "title", Type: "text", Searchable: true},
			{Name: "adopted", Type: "text"},
			{Name: "rev", Type: "integer"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	if err := s.Upsert(ctx, "meetings", Document{"id": "m1", "title": "週會 甲標記", "adopted": "", "rev": 1}); err != nil {
		t.Fatalf("seed upsert: %v", err)
	}
	return s, dbPath
}

func readMeeting(t *testing.T, dbPath, id string) map[string]interface{} {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	var title, adopted string
	var rev int64
	err = db.QueryRow("SELECT title, adopted, rev FROM meetings WHERE id = ?", id).Scan(&title, &adopted, &rev)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		t.Fatalf("read meeting: %v", err)
	}
	return map[string]interface{}{"title": title, "adopted": adopted, "rev": rev}
}

func meetingHits(t *testing.T, s *Store, term string) int {
	t.Helper()
	rows, err := s.Query(context.Background(), "meetings", Node{Search: &SearchQuery{Term: term}}, Result{})
	if err != nil {
		t.Fatalf("Query(%q): %v", term, err)
	}
	return len(rows)
}

func TestUpdateChangesOnlyGivenFieldsWhenConditionHolds(t *testing.T) {
	s, dbPath := openUpdateTestStore(t)

	err := s.Update(context.Background(), "meetings",
		Document{"id": "m1", "adopted": "v2"},
		[]Condition{{Field: "adopted", Operator: "=", Value: ""}})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := readMeeting(t, dbPath, "m1")
	if got["adopted"] != "v2" || got["title"] != "週會 甲標記" || got["rev"] != int64(1) {
		t.Fatalf("after update the row is %v; want only adopted changed", got)
	}
}

func TestUpdateWithoutConditionsUpdates(t *testing.T) {
	s, dbPath := openUpdateTestStore(t)

	if err := s.Update(context.Background(), "meetings", Document{"id": "m1", "rev": 2}, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := readMeeting(t, dbPath, "m1"); got["rev"] != int64(2) {
		t.Fatalf("rev = %v, want 2", got["rev"])
	}
}

func TestUpdateConflictLeavesRowAndReportsCurrent(t *testing.T) {
	s, dbPath := openUpdateTestStore(t)
	ctx := context.Background()

	if err := s.Update(ctx, "meetings", Document{"id": "m1", "adopted": "B"}, nil); err != nil {
		t.Fatalf("setup update: %v", err)
	}

	err := s.Update(ctx, "meetings",
		Document{"id": "m1", "adopted": "A"},
		[]Condition{
			{Field: "rev", Operator: ">=", Value: 1},
			{Field: "adopted", Operator: "=", Value: ""},
		})

	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("got %v, want a *ConflictError", err)
	}
	if !IsConflict(err) {
		t.Fatal("IsConflict does not recognise a *ConflictError")
	}
	if len(conflict.Failed) != 1 || conflict.Failed[0].Field != "adopted" {
		t.Fatalf("Failed = %+v, want only the adopted condition", conflict.Failed)
	}
	if conflict.Current["adopted"] != "B" || conflict.Current["id"] != "m1" {
		t.Fatalf("Current = %v, want the stored document with adopted=B", conflict.Current)
	}
	if got := readMeeting(t, dbPath, "m1"); got["adopted"] != "B" {
		t.Fatalf("adopted = %v after a conflict, want B", got["adopted"])
	}
}

func TestUpdateMissingDocumentIsNotFound(t *testing.T) {
	s, dbPath := openUpdateTestStore(t)

	err := s.Update(context.Background(), "meetings", Document{"id": "nope", "adopted": "A"}, nil)
	if !IsNotFound(err) {
		t.Fatalf("got %v, want a NotFoundError", err)
	}
	if readMeeting(t, dbPath, "nope") != nil {
		t.Fatal("Update created a document that did not exist")
	}

	err = s.Update(context.Background(), "meetings", Document{"id": "nope", "adopted": "A"},
		[]Condition{{Field: "adopted", Operator: "=", Value: ""}})
	if !IsNotFound(err) {
		t.Fatalf("with a condition: got %v, want a NotFoundError", err)
	}
}

func TestUpdateRejectsInvalidRequests(t *testing.T) {
	s, _ := openUpdateTestStore(t)
	ctx := context.Background()

	for name, call := range map[string]func() error{
		"unknown where field": func() error {
			return s.Update(ctx, "meetings", Document{"id": "m1", "adopted": "A"},
				[]Condition{{Field: "nope", Operator: "=", Value: ""}})
		},
		"unsupported operator": func() error {
			return s.Update(ctx, "meetings", Document{"id": "m1", "adopted": "A"},
				[]Condition{{Field: "adopted", Operator: "~", Value: ""}})
		},
		"unknown document field": func() error {
			return s.Update(ctx, "meetings", Document{"id": "m1", "nope": "A"}, nil)
		},
		"missing primary key": func() error {
			return s.Update(ctx, "meetings", Document{"adopted": "A"}, nil)
		},
	} {
		if err := call(); !IsValidation(err) {
			t.Errorf("%s: got %v, want a ValidationError", name, err)
		}
	}

	if err := s.Update(ctx, "nope", Document{"id": "m1"}, nil); !IsNotFound(err) {
		t.Errorf("unknown collection: got %v, want a NotFoundError", err)
	}
}

func TestUpdateOfSearchableFieldReindexes(t *testing.T) {
	s, _ := openUpdateTestStore(t)

	if err := s.Update(context.Background(), "meetings", Document{"id": "m1", "title": "月會 乙標記"}, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if n := meetingHits(t, s, "乙標記"); n != 1 {
		t.Fatalf("new term hits %d documents, want 1", n)
	}
	if n := meetingHits(t, s, "甲標記"); n != 0 {
		t.Fatalf("old term still hits %d documents, want 0", n)
	}
}

// TestConcurrentConditionalUpdatesAdmitOne reproduces the paper_workers race:
// several writers read "nothing adopted yet" and each tries to adopt its own
// version. Exactly one may win; the rest must see a conflict.
func TestConcurrentConditionalUpdatesAdmitOne(t *testing.T) {
	s, dbPath := openUpdateTestStore(t)

	const writers = 8
	var wg sync.WaitGroup
	results := make(chan error, writers)
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results <- s.Update(context.Background(), "meetings",
				Document{"id": "m1", "adopted": string(rune('A' + i))},
				[]Condition{{Field: "adopted", Operator: "=", Value: ""}})
		}(i)
	}
	wg.Wait()
	close(results)

	wins, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case IsConflict(err):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || conflicts != writers-1 {
		t.Fatalf("%d wins and %d conflicts, want 1 and %d", wins, conflicts, writers-1)
	}
	if got := readMeeting(t, dbPath, "m1"); got["adopted"] == "" {
		t.Fatal("no version was adopted")
	}
}
