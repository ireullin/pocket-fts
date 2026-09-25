package pocketfts

import (
	"errors"
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

// requireValidationError 斷言錯誤存在且屬於 ValidationError，handler 才會回 400。
func requireValidationError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected a *ValidationError, got %T: %v", err, err)
	}
}

// TestValidateResultFieldsRejectsUnknownField 確認寫錯的欄位名稱會回報錯誤。
// result.fields 過去只檢查識別字格式，不對照 schema，於是被串進 SELECT，
// 由 SQLite 回報錯誤，呼叫端拿到 HTTP 500 而不是 400。
func TestValidateResultFieldsRejectsUnknownField(t *testing.T) {
	requireValidationError(t, validateResultFields([]string{"title", "no_such_column"}, testSchema(), false))
}

func TestValidateResultFieldsAcceptsKnownFields(t *testing.T) {
	if err := validateResultFields([]string{"id", "title", "created_at"}, testSchema(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// SQLite 的欄位名稱不分大小寫，SELECT ID 取得的是宣告為 id 的那一欄。
// 驗證若用精確比對，只是大小寫不同的請求會從 200 變成 400。
func TestValidateResultFieldsIsCaseInsensitive(t *testing.T) {
	if err := validateResultFields([]string{"ID", "Title"}, testSchema(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateOrderByIsCaseInsensitive(t *testing.T) {
	if _, err := validateOrderBy([]OrderBySpec{{Field: "Created_At", Direction: "desc"}}, testSchema()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResultFieldsAcceptsStar(t *testing.T) {
	if err := validateResultFields([]string{"*"}, testSchema(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResultFieldsAcceptsEmptyList(t *testing.T) {
	if err := validateResultFields(nil, testSchema(), false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResultFieldsRejectsInvalidIdentifier(t *testing.T) {
	requireValidationError(t, validateResultFields([]string{"title; DROP TABLE documents"}, testSchema(), false))
}

// _score 的規則比照 order_by：只有帶 search 子句的查詢才產生分數。
func TestValidateResultFieldsAcceptsScoreWithSearch(t *testing.T) {
	if err := validateResultFields([]string{"id", scoreField}, testSchema(), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateResultFieldsRejectsScoreWithoutSearch(t *testing.T) {
	requireValidationError(t, validateResultFields([]string{"id", scoreField}, testSchema(), false))
}

// 分數是取回記錄之後依主鍵補上的。少了主鍵就補不上，靜默回一份沒有分數的結果
// 會讓呼叫端看不出自己漏了什麼。
func TestValidateResultFieldsRejectsScoreWithoutPrimaryKey(t *testing.T) {
	requireValidationError(t, validateResultFields([]string{"title", scoreField}, testSchema(), true))
}

func TestValidateResultFieldsAcceptsScoreWithStar(t *testing.T) {
	if err := validateResultFields([]string{"*", scoreField}, testSchema(), true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestSelectableFieldsDropsScore 確認 _score 不會被寫進 SELECT。
func TestSelectableFieldsDropsScore(t *testing.T) {
	got := selectableFields([]string{"id", scoreField, "title"})
	want := []string{"id", "title"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
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
