package main

import (
	"errors"
	"math"
	"testing"
)

func testSchema() *CollectionSchema {
	return &CollectionSchema{
		Name:       "documents",
		PrimaryKey: "id",
		Fields: []Field{
			{Name: "id", Type: "text"},
			{Name: "title", Type: "text"},
			{Name: "created_at", Type: "integer"},
		},
	}
}

// TestValidateOrderByRejectsUnknownField 確認寫錯的欄位名稱會回報錯誤，
// 而不是被靜默丟掉。呼叫端過去無從發現自己弄錯。
func TestValidateOrderByRejectsUnknownField(t *testing.T) {
	_, err := validateOrderBy([]OrderBySpec{{Field: "no_such_column", Direction: "desc"}}, testSchema())
	if err == nil {
		t.Fatal("expected an error for an unknown order_by field, got nil")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

func TestValidateOrderByAcceptsKnownFields(t *testing.T) {
	usesScore, err := validateOrderBy([]OrderBySpec{
		{Field: "created_at", Direction: "desc"},
		{Field: "id", Direction: "asc"},
	}, testSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if usesScore {
		t.Fatal("expected usesScore to be false")
	}
}

func TestValidateOrderByReportsScoreUsage(t *testing.T) {
	usesScore, err := validateOrderBy([]OrderBySpec{{Field: "_score", Direction: "desc"}}, testSchema())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !usesScore {
		t.Fatal("expected usesScore to be true for _score")
	}
}

func TestValidateOrderByRejectsBadDirection(t *testing.T) {
	if _, err := validateOrderBy([]OrderBySpec{{Field: "created_at", Direction: "sideways"}}, testSchema()); err == nil {
		t.Fatal("expected an error for an invalid direction, got nil")
	}
}

func TestBuildOrderByClause(t *testing.T) {
	cases := []struct {
		name    string
		orderBy []OrderBySpec
		want    string
	}{
		{"empty", nil, ""},
		{"default direction is asc", []OrderBySpec{{Field: "created_at"}}, " ORDER BY created_at ASC"},
		{"desc", []OrderBySpec{{Field: "created_at", Direction: "DESC"}}, " ORDER BY created_at DESC"},
		{"multiple keys", []OrderBySpec{
			{Field: "created_at", Direction: "desc"},
			{Field: "id", Direction: "asc"},
		}, " ORDER BY created_at DESC, id ASC"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildOrderByClause(tc.orderBy); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildLimitClauseOffsetOnly 確認只給 offset 時仍產生合法的 SQL。
// SQLite 的 OFFSET 必須跟在 LIMIT 後面。
func TestBuildLimitClauseOffsetOnly(t *testing.T) {
	if got, want := buildLimitClause(0, 3), " LIMIT -1 OFFSET 3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := buildLimitClause(8, 0), " LIMIT 8"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := buildLimitClause(8, 3), " LIMIT 8 OFFSET 3"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got, want := buildLimitClause(0, 0), ""; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func idsOf(records []map[string]interface{}) []string {
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = record["id"].(string)
	}
	return ids
}

func sameIDs(got []string, want ...string) bool {
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

func TestSortRecordsByColumn(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "b", "created_at": int64(20)},
		{"id": "a", "created_at": int64(30)},
		{"id": "c", "created_at": int64(10)},
	}

	sortRecords(records, []OrderBySpec{{Field: "created_at", Direction: "desc"}}, false)
	if got := idsOf(records); !sameIDs(got, "a", "b", "c") {
		t.Fatalf("desc: got %v, want [a b c]", got)
	}

	sortRecords(records, []OrderBySpec{{Field: "created_at", Direction: "asc"}}, false)
	if got := idsOf(records); !sameIDs(got, "c", "b", "a") {
		t.Fatalf("asc: got %v, want [c b a]", got)
	}
}

// TestSortRecordsByScoreIsRelevanceOrder 確認 _score 依相關性解讀：
// ftscore 的分數越小越相關，所以 desc 代表最相關在前。
func TestSortRecordsByScoreIsRelevanceOrder(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "weak", "_score": -1.0},
		{"id": "strong", "_score": -3.0},
		{"id": "middle", "_score": -2.0},
	}

	sortRecords(records, []OrderBySpec{{Field: "_score", Direction: "desc"}}, true)
	if got := idsOf(records); !sameIDs(got, "strong", "middle", "weak") {
		t.Fatalf("desc: got %v, want [strong middle weak]", got)
	}

	sortRecords(records, []OrderBySpec{{Field: "_score", Direction: "asc"}}, true)
	if got := idsOf(records); !sameIDs(got, "weak", "middle", "strong") {
		t.Fatalf("asc: got %v, want [weak middle strong]", got)
	}
}

// TestSortRecordsDefaultsToRelevance 確認沒有給 order_by 但結果帶分數時，
// 維持既有的相關性排序。
func TestSortRecordsDefaultsToRelevance(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "weak", "_score": -1.0},
		{"id": "strong", "_score": -3.0},
	}
	sortRecords(records, nil, true)
	if got := idsOf(records); !sameIDs(got, "strong", "weak") {
		t.Fatalf("got %v, want [strong weak]", got)
	}
}

// TestSortRecordsPutsUnscoredLast 確認沒有分數的記錄視為相關性最低。
func TestSortRecordsPutsUnscoredLast(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "unscored"},
		{"id": "scored", "_score": -1.0},
	}
	sortRecords(records, []OrderBySpec{{Field: "_score", Direction: "desc"}}, true)
	if got := idsOf(records); !sameIDs(got, "scored", "unscored") {
		t.Fatalf("got %v, want [scored unscored]", got)
	}
}

// TestSortRecordsMultipleKeys 確認第一個鍵相同時用第二個鍵決勝。
func TestSortRecordsMultipleKeys(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "b", "status": "done", "created_at": int64(1)},
		{"id": "a", "status": "done", "created_at": int64(2)},
		{"id": "c", "status": "draft", "created_at": int64(9)},
	}
	sortRecords(records, []OrderBySpec{
		{Field: "status", Direction: "asc"},
		{Field: "created_at", Direction: "desc"},
	}, false)
	if got := idsOf(records); !sameIDs(got, "a", "b", "c") {
		t.Fatalf("got %v, want [a b c]", got)
	}
}

// TestCompareValuesFollowsSQLiteTypeOrder 確認跨型別比較沿用 SQLite 的順序：
// NULL < 數值 < 文字 < BLOB。
func TestCompareValuesFollowsSQLiteTypeOrder(t *testing.T) {
	ordered := []interface{}{nil, int64(1), float64(2.5), "abc", []byte{0x01}}
	for i := 0; i < len(ordered)-1; i++ {
		if got := compareValues(ordered[i], ordered[i+1]); got != -1 {
			t.Fatalf("compareValues(%v, %v) = %d, want -1", ordered[i], ordered[i+1], got)
		}
		if got := compareValues(ordered[i+1], ordered[i]); got != 1 {
			t.Fatalf("compareValues(%v, %v) = %d, want 1", ordered[i+1], ordered[i], got)
		}
	}
	if got := compareValues(nil, nil); got != 0 {
		t.Fatalf("compareValues(nil, nil) = %d, want 0", got)
	}
}

// TestCompareValuesKeepsLargeIntegerPrecision 確認大整數不會因為轉成
// float64 而比成相等。
func TestCompareValuesKeepsLargeIntegerPrecision(t *testing.T) {
	a := int64(math.MaxInt64)
	b := int64(math.MaxInt64 - 1)
	if got := compareValues(a, b); got != 1 {
		t.Fatalf("compareValues(%d, %d) = %d, want 1", a, b, got)
	}
}

func TestApplyLimitOffset(t *testing.T) {
	records := []map[string]interface{}{
		{"id": "a"}, {"id": "b"}, {"id": "c"}, {"id": "d"},
	}

	if got := idsOf(applyLimitOffset(records, 2, 0)); !sameIDs(got, "a", "b") {
		t.Fatalf("limit only: got %v", got)
	}
	if got := idsOf(applyLimitOffset(records, 2, 2)); !sameIDs(got, "c", "d") {
		t.Fatalf("limit+offset: got %v", got)
	}
	if got := idsOf(applyLimitOffset(records, 0, 3)); !sameIDs(got, "d") {
		t.Fatalf("offset only: got %v", got)
	}
	if got := applyLimitOffset(records, 2, 10); len(got) != 0 {
		t.Fatalf("offset past end: got %v", got)
	}
}

// TestIsValidIdentifierIsAnchored 確認識別字檢查頭尾都錨定，
// 不會讓帶有 SQL 片段的字串通過。
func TestIsValidIdentifierIsAnchored(t *testing.T) {
	valid := []string{"id", "created_at", "Table1", "_score1"}
	for _, name := range valid {
		if !isValidIdentifier(name) {
			t.Fatalf("isValidIdentifier(%q) = false, want true", name)
		}
	}

	invalid := []string{"", "id; DROP TABLE documents--", "created_at DESC", "a b", "a-b", "a.b"}
	for _, name := range invalid {
		if isValidIdentifier(name) {
			t.Fatalf("isValidIdentifier(%q) = true, want false", name)
		}
	}
}

// TestQueryHasSearch 確認巢狀查詢裡的 search 節點都找得到。
func TestQueryHasSearch(t *testing.T) {
	if queryHasSearch(nil) {
		t.Fatal("nil node should not report a search clause")
	}
	if queryHasSearch(&QueryNode{SQL: &SQLQuery{}}) {
		t.Fatal("sql-only node should not report a search clause")
	}
	if !queryHasSearch(&QueryNode{Search: &SearchQuery{Term: "x"}}) {
		t.Fatal("search node should report a search clause")
	}
	nested := &QueryNode{And: []*QueryNode{
		{SQL: &SQLQuery{}},
		{Or: []*QueryNode{{Search: &SearchQuery{Term: "x"}}}},
	}}
	if !queryHasSearch(nested) {
		t.Fatal("nested search node should report a search clause")
	}
}
