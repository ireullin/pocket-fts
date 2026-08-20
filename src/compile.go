package main

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// sqlCompiler 把查詢樹編譯成單一 SQL WHERE 運算式。
//
// FTS 索引是獨立於 db.sqlite 的第二個資料庫，無法用一條 SQL 同時比對全文與
// SQL 條件。編譯器的做法是先向 ftscore 取得命中的主鍵，再把那份主鍵清單以
// 單一 JSON 參數傳進 SQL，用 json_each 展開成子查詢。這樣整棵查詢樹就能收斂
// 成一條 SQL，ORDER BY 與 LIMIT 也能交給 SQLite。
//
// 舊做法把每個主鍵綁成一個 SQL 參數，符合筆數超過 SQLITE_MAX_VARIABLE_NUMBER
// （SQLite 3.32 之後預設 32766）就會失敗。json_each 只用一個參數，沒有這個上限。
type sqlCompiler struct {
	qe         *QueryExecutor
	collection string
	primaryKey string
	scores     ScoreMap
	searches   int
}

func newSQLCompiler(qe *QueryExecutor, collection, primaryKey string) *sqlCompiler {
	return &sqlCompiler{
		qe:         qe,
		collection: collection,
		primaryKey: primaryKey,
		scores:     make(ScoreMap),
	}
}

// compile 產生 WHERE 子句與對應的參數。參數的順序與子句中 ? 出現的順序一致。
func (c *sqlCompiler) compile(node *QueryNode) (string, []interface{}, error) {
	if node == nil {
		return "", nil, fmt.Errorf("empty query node")
	}

	switch {
	case node.And != nil:
		return c.compileGroup(node.And, "AND")

	case node.Or != nil:
		return c.compileGroup(node.Or, "OR")

	case node.Not != nil:
		inner, args, err := c.compile(node.Not)
		if err != nil {
			return "", nil, err
		}
		// COALESCE 讓內層條件算出 NULL 的列被保留。原本的做法是「全部主鍵扣掉
		// 命中的主鍵」，欄位為 NULL 的列不會被扣掉；直接寫 NOT (...) 會把它們
		// 一併排除，語意就變了。
		return "NOT COALESCE((" + inner + "), 0)", args, nil

	case node.SQL != nil:
		where, args, err := c.qe.buildWhereClause(node.SQL.Where)
		if err != nil {
			return "", nil, err
		}
		if where == "" {
			return "1", nil, nil
		}
		return "(" + where + ")", args, nil

	case node.Search != nil:
		return c.compileSearch(node.Search)
	}

	return "", nil, fmt.Errorf("empty query node")
}

func (c *sqlCompiler) compileGroup(nodes []*QueryNode, op string) (string, []interface{}, error) {
	if len(nodes) == 0 {
		if op == "AND" {
			return "1", nil, nil
		}
		return "0", nil, nil
	}

	parts := make([]string, 0, len(nodes))
	var args []interface{}
	for _, node := range nodes {
		clause, nodeArgs, err := c.compile(node)
		if err != nil {
			return "", nil, err
		}
		parts = append(parts, clause)
		args = append(args, nodeArgs...)
	}

	return "(" + strings.Join(parts, " "+op+" ") + ")", args, nil
}

// compileSearch 執行全文搜尋，把命中的主鍵編成一個 json_each 子查詢。
//
// 這條路徑要全部命中，不能只取前 N 筆：後面還有 SQL 篩選與依欄位排序，
// 先截斷會讓結果安靜地少掉大半。
func (c *sqlCompiler) compileSearch(query *SearchQuery) (string, []interface{}, error) {
	results, err := c.qe.executeSearchQuery(query, c.collection, allHits)
	if err != nil {
		return "", nil, err
	}
	c.searches++

	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.PrimaryKey
		c.scores[result.PrimaryKey] = result.Score
	}

	if len(ids) == 0 {
		return "0", nil, nil
	}

	blob, err := json.Marshal(ids)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode search result ids: %w", err)
	}

	clause := fmt.Sprintf("%s IN (SELECT value FROM json_each(?))", c.primaryKey)
	return clause, []interface{}{string(blob)}, nil
}

// buildIDClause 產生一個「主鍵落在這份清單裡」的子句，同樣只用一個參數。
func buildIDClause(primaryKey string, ids []string) (string, []interface{}, error) {
	if len(ids) == 0 {
		return "0", nil, nil
	}
	blob, err := json.Marshal(ids)
	if err != nil {
		return "", nil, fmt.Errorf("failed to encode ids: %w", err)
	}
	return fmt.Sprintf("%s IN (SELECT value FROM json_each(?))", primaryKey),
		[]interface{}{string(blob)}, nil
}

// ftsHeap 是依分數排序的最大堆，用來挑出分數最小（最相關）的前 k 筆。
type ftsHeap []FTSResult

func (h ftsHeap) Len() int            { return len(h) }
func (h ftsHeap) Less(i, j int) bool  { return h[i].Score > h[j].Score }
func (h ftsHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *ftsHeap) Push(x interface{}) { *h = append(*h, x.(FTSResult)) }
func (h *ftsHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// topKRelevant 取出最相關的前 k 筆，並依相關性由高到低排列。
// ftscore 的分數越小越相關，所以取的是分數最小的 k 筆。
// k <= 0 或 k >= 總數時退回完整排序。
func topKRelevant(results []FTSResult, k int) []FTSResult {
	if k <= 0 || k >= len(results) {
		out := append([]FTSResult(nil), results...)
		sort.SliceStable(out, func(i, j int) bool { return out[i].Score < out[j].Score })
		return out
	}

	h := make(ftsHeap, 0, k)
	heap.Init(&h)
	for _, result := range results {
		if h.Len() < k {
			heap.Push(&h, result)
			continue
		}
		if result.Score < h[0].Score {
			h[0] = result
			heap.Fix(&h, 0)
		}
	}

	out := make([]FTSResult, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(&h).(FTSResult)
	}
	return out
}
