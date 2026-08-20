package main

import (
	"strings"
	"testing"
)

func compileTree(t *testing.T, node *QueryNode) (string, []interface{}) {
	t.Helper()
	c := newSQLCompiler(&QueryExecutor{}, "docs", "id")
	where, args, err := c.compile(node)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	return where, args
}

func TestCompileSQLNode(t *testing.T) {
	where, args := compileTree(t, &QueryNode{
		SQL: &SQLQuery{Where: map[string]interface{}{"status": "done"}},
	})
	if where != "(status = ?)" {
		t.Fatalf("got %q", where)
	}
	if len(args) != 1 || args[0] != "done" {
		t.Fatalf("got args %v", args)
	}
}

// TestCompileEmptySQLMatchesEverything 確認空的 where 條件編譯成恆真，
// 而不是空字串（空字串會讓外層的 AND/OR 組合出壞掉的 SQL）。
func TestCompileEmptySQLMatchesEverything(t *testing.T) {
	where, args := compileTree(t, &QueryNode{
		SQL: &SQLQuery{Where: map[string]interface{}{}},
	})
	if where != "1" {
		t.Fatalf("got %q, want \"1\"", where)
	}
	if len(args) != 0 {
		t.Fatalf("got args %v", args)
	}
}

func TestCompileAndOr(t *testing.T) {
	node := &QueryNode{And: []*QueryNode{
		{SQL: &SQLQuery{Where: map[string]interface{}{"status": "done"}}},
		{Or: []*QueryNode{
			{SQL: &SQLQuery{Where: map[string]interface{}{"created_at": map[string]interface{}{"$gt": 5}}}},
			{SQL: &SQLQuery{Where: map[string]interface{}{"created_at": map[string]interface{}{"$lt": 1}}}},
		}},
	}}
	where, args := compileTree(t, node)
	want := "((status = ?) AND ((created_at > ?) OR (created_at < ?)))"
	if where != want {
		t.Fatalf("got %q, want %q", where, want)
	}
	// 參數順序必須與子句中 ? 出現的順序一致。
	if len(args) != 3 || args[0] != "done" || args[1] != 5 || args[2] != 1 {
		t.Fatalf("got args %v", args)
	}
}

// TestCompileNotKeepsNullRows 確認 $not 用 COALESCE 包起來。
// 舊做法是「全部主鍵扣掉命中的主鍵」，欄位為 NULL 的列不會被扣掉；
// 直接寫 NOT (...) 會把它們一併排除，語意就變了。
func TestCompileNotKeepsNullRows(t *testing.T) {
	where, _ := compileTree(t, &QueryNode{
		Not: &QueryNode{SQL: &SQLQuery{Where: map[string]interface{}{"status": "done"}}},
	})
	if !strings.HasPrefix(where, "NOT COALESCE(") {
		t.Fatalf("got %q, want a COALESCE-wrapped NOT", where)
	}
}

func TestCompileEmptyGroups(t *testing.T) {
	if where, _ := compileTree(t, &QueryNode{And: []*QueryNode{}}); where != "1" {
		t.Fatalf("empty $and: got %q, want \"1\"", where)
	}
	if where, _ := compileTree(t, &QueryNode{Or: []*QueryNode{}}); where != "0" {
		t.Fatalf("empty $or: got %q, want \"0\"", where)
	}
}

func TestCompileRejectsEmptyNode(t *testing.T) {
	c := newSQLCompiler(&QueryExecutor{}, "docs", "id")
	if _, _, err := c.compile(&QueryNode{}); err == nil {
		t.Fatal("expected an error for an empty node")
	}
}

// TestBuildIDClauseUsesOneParameter 確認主鍵清單只吃一個 bind 參數。
// 舊做法每個主鍵一個參數，超過 SQLite 的 32766 上限就整個查詢失敗。
func TestBuildIDClauseUsesOneParameter(t *testing.T) {
	ids := make([]string, 100000)
	for i := range ids {
		ids[i] = DocIDForTest(i)
	}
	where, args, err := buildIDClause("id", ids)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if args == nil || len(args) != 1 {
		t.Fatalf("got %d args, want exactly 1", len(args))
	}
	if !strings.Contains(where, "json_each(?)") {
		t.Fatalf("got %q, want a json_each subquery", where)
	}
	if strings.Count(where, "?") != 1 {
		t.Fatalf("got %d placeholders in %q, want 1", strings.Count(where, "?"), where)
	}
}

func TestBuildIDClauseEmpty(t *testing.T) {
	where, args, err := buildIDClause("id", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if where != "0" || len(args) != 0 {
		t.Fatalf("got %q with %d args, want \"0\" with none", where, len(args))
	}
}

func DocIDForTest(i int) string {
	return "d" + strings.Repeat("0", 8-len(itoa(i))) + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestTopKRelevantPicksSmallestScores 確認取的是分數最小（最相關）的前 k 筆，
// 而且回傳順序是相關性由高到低。
func TestTopKRelevantPicksSmallestScores(t *testing.T) {
	results := []FTSResult{
		{PrimaryKey: "e", Score: -1.0},
		{PrimaryKey: "a", Score: -5.0},
		{PrimaryKey: "c", Score: -3.0},
		{PrimaryKey: "b", Score: -4.0},
		{PrimaryKey: "d", Score: -2.0},
	}

	top := topKRelevant(results, 3)
	if len(top) != 3 {
		t.Fatalf("got %d results, want 3", len(top))
	}
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if top[i].PrimaryKey != w {
			t.Fatalf("position %d: got %q, want %q (full: %v)", i, top[i].PrimaryKey, w, keysOf(top))
		}
	}
}

func TestTopKRelevantHandlesKBeyondLength(t *testing.T) {
	results := []FTSResult{
		{PrimaryKey: "b", Score: -1.0},
		{PrimaryKey: "a", Score: -2.0},
	}
	for _, k := range []int{0, 2, 5, -1} {
		top := topKRelevant(results, k)
		if len(top) != 2 {
			t.Fatalf("k=%d: got %d results, want 2", k, len(top))
		}
		if top[0].PrimaryKey != "a" || top[1].PrimaryKey != "b" {
			t.Fatalf("k=%d: got %v, want [a b]", k, keysOf(top))
		}
	}
}

func keysOf(results []FTSResult) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.PrimaryKey
	}
	return out
}

// TestIsRelevanceDescOrder 確認只有「未指定」與「_score desc」會走相關性快速路徑。
func TestIsRelevanceDescOrder(t *testing.T) {
	cases := []struct {
		name    string
		orderBy []OrderBySpec
		want    bool
	}{
		{"未指定", nil, true},
		{"_score desc", []OrderBySpec{{Field: "_score", Direction: "desc"}}, true},
		{"_score DESC 大寫", []OrderBySpec{{Field: "_score", Direction: "DESC"}}, true},
		{"_score asc", []OrderBySpec{{Field: "_score", Direction: "asc"}}, false},
		{"_score 省略方向", []OrderBySpec{{Field: "_score"}}, false},
		{"一般欄位", []OrderBySpec{{Field: "created_at", Direction: "desc"}}, false},
		{"多鍵", []OrderBySpec{{Field: "_score", Direction: "desc"}, {Field: "id"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRelevanceDescOrder(tc.orderBy); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
