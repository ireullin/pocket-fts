package pocketfts

import (
	"context"
	"path/filepath"
	"testing"
)

// These tests pin the relevance ordering that fetchRecords pushes into SQL:
// _score desc means most relevant (smallest ftscore score) first, unscored
// rows count as least relevant, and ties break on the primary key.

func openScoreOrderStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(Config{Path: filepath.Join(t.TempDir(), "test.sqlite")})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	ctx := context.Background()
	err = s.CreateCollection(ctx, Schema{
		Name:       "docs",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "body", Type: "text", Searchable: true},
			{Name: "status", Type: "text"},
			{Name: "n", Type: "integer"},
		},
	})
	if err != nil {
		t.Fatalf("CreateCollection failed: %v", err)
	}
	for _, doc := range []Document{
		{"id": "strong", "body": "標記 標記 標記", "status": "done", "n": 1},
		{"id": "middle", "body": "標記 標記 填充", "status": "done", "n": 2},
		{"id": "weak", "body": "標記 填充 填充 填充 填充", "status": "done", "n": 3},
		{"id": "twin_b", "body": "標記 雙胞", "status": "draft", "n": 5},
		{"id": "twin_a", "body": "標記 雙胞", "status": "draft", "n": 4},
		{"id": "plain_2", "body": "無關", "status": "done", "n": 7},
		{"id": "plain_1", "body": "無關", "status": "done", "n": 6},
	} {
		if err := s.Upsert(ctx, "docs", doc); err != nil {
			t.Fatalf("upsert %v: %v", doc["id"], err)
		}
	}
	return s
}

func queryRows(t *testing.T, s *Store, q Node, r Result) []map[string]interface{} {
	t.Helper()
	rows, err := s.Query(context.Background(), "docs", q, r)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	return rows
}

func rowIDs(rows []map[string]interface{}) []string {
	ids := make([]string, len(rows))
	for i, row := range rows {
		ids[i] = row["id"].(string)
	}
	return ids
}

func equalIDs(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

var markerSearch = Node{Search: &SearchQuery{Term: "標記"}}

// markerAndDone makes the query take the general path (search plus SQL),
// not the single-search fast path.
var markerAndDone = Node{And: []*Node{
	{Search: &SearchQuery{Term: "標記"}},
	{SQL: &SQLQuery{Where: map[string]interface{}{"status": "done"}}},
}}

func TestScoreDescIsMostRelevantFirst(t *testing.T) {
	s := openScoreOrderStore(t)

	rows := queryRows(t, s, markerAndDone, Result{OrderBy: []OrderBySpec{{Field: "_score", Direction: "desc"}}})
	if got := rowIDs(rows); !equalIDs(got, "strong", "middle", "weak") {
		t.Fatalf("desc: got %v, want [strong middle weak]", got)
	}
	for i := 1; i < len(rows); i++ {
		if rows[i-1]["_score"].(float64) > rows[i]["_score"].(float64) {
			t.Fatalf("desc: scores are not ascending: %v", rows)
		}
	}

	rows = queryRows(t, s, markerAndDone, Result{OrderBy: []OrderBySpec{{Field: "_score", Direction: "asc"}}})
	if got := rowIDs(rows); !equalIDs(got, "weak", "middle", "strong") {
		t.Fatalf("asc: got %v, want [weak middle strong]", got)
	}
}

func TestScoreOrderIsTheDefaultWithSearch(t *testing.T) {
	s := openScoreOrderStore(t)

	rows := queryRows(t, s, markerAndDone, Result{})
	if got := rowIDs(rows); !equalIDs(got, "strong", "middle", "weak") {
		t.Fatalf("got %v, want [strong middle weak]", got)
	}
}

func TestScoreOrderPagesInSQL(t *testing.T) {
	s := openScoreOrderStore(t)

	rows := queryRows(t, s, markerAndDone, Result{Limit: 1, Offset: 1})
	if got := rowIDs(rows); !equalIDs(got, "middle") {
		t.Fatalf("limit 1 offset 1: got %v, want [middle]", got)
	}
	rows = queryRows(t, s, markerAndDone, Result{Offset: 2})
	if got := rowIDs(rows); !equalIDs(got, "weak") {
		t.Fatalf("offset 2: got %v, want [weak]", got)
	}
	rows = queryRows(t, s, markerAndDone, Result{Limit: 5, Offset: 10})
	if len(rows) != 0 {
		t.Fatalf("offset past the end: got %v", rowIDs(rows))
	}
}

// TestUnscoredRowsCountAsLeastRelevant covers rows that match an $or without
// matching its search: they carry no _score, sort last for desc and first
// for asc, and among themselves by primary key.
func TestUnscoredRowsCountAsLeastRelevant(t *testing.T) {
	s := openScoreOrderStore(t)
	mixed := Node{Or: []*Node{
		{And: []*Node{
			{Search: &SearchQuery{Term: "標記"}},
			{SQL: &SQLQuery{Where: map[string]interface{}{"status": "done"}}},
		}},
		{SQL: &SQLQuery{Where: map[string]interface{}{"n": map[string]interface{}{"$gte": 6}}}},
	}}

	rows := queryRows(t, s, mixed, Result{OrderBy: []OrderBySpec{{Field: "_score", Direction: "desc"}}})
	if got := rowIDs(rows); !equalIDs(got, "strong", "middle", "weak", "plain_1", "plain_2") {
		t.Fatalf("desc: got %v, want scored rows then [plain_1 plain_2]", got)
	}
	for _, row := range rows[3:] {
		if _, ok := row["_score"]; ok {
			t.Fatalf("unscored row %v carries _score", row["id"])
		}
	}

	rows = queryRows(t, s, mixed, Result{OrderBy: []OrderBySpec{{Field: "_score", Direction: "asc"}}})
	if got := rowIDs(rows); !equalIDs(got, "plain_1", "plain_2", "weak", "middle", "strong") {
		t.Fatalf("asc: got %v, want [plain_1 plain_2] then least relevant first", got)
	}
}

func TestScoreTiesBreakOnNextKeyThenPrimaryKey(t *testing.T) {
	s := openScoreOrderStore(t)
	twins := Node{And: []*Node{
		{Search: &SearchQuery{Term: "雙胞"}},
		{SQL: &SQLQuery{Where: map[string]interface{}{"status": "draft"}}},
	}}

	rows := queryRows(t, s, twins, Result{OrderBy: []OrderBySpec{
		{Field: "_score", Direction: "desc"},
		{Field: "n", Direction: "desc"},
	}})
	if got := rowIDs(rows); !equalIDs(got, "twin_b", "twin_a") {
		t.Fatalf("second key n desc: got %v, want [twin_b twin_a]", got)
	}

	rows = queryRows(t, s, twins, Result{})
	if got := rowIDs(rows); !equalIDs(got, "twin_a", "twin_b") {
		t.Fatalf("tie on score: got %v, want primary key order [twin_a twin_b]", got)
	}
}

func TestScoreSetsAreReleased(t *testing.T) {
	s := openScoreOrderStore(t)
	queryRows(t, s, markerAndDone, Result{Limit: 2})
	queryRows(t, s, markerSearch, Result{Limit: 2})

	leaked := 0
	scoreSets.Range(func(_, _ any) bool { leaked++; return true })
	if leaked != 0 {
		t.Fatalf("%d score sets are still registered after the queries finished", leaked)
	}
}
