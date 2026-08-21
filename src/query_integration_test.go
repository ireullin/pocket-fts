package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"testing"
)

// setupQueryEngine 起一個完整的查詢堆疊：SQLite、ftscore 引擎與查詢執行器。
// 測試透過真正的 HTTP handler 發請求，涵蓋從解析到回應的整條路徑。
func setupQueryEngine(t *testing.T) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	logger = slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := LoadFTSLibrary(dbPath); err != nil {
		t.Fatalf("failed to load ftscore: %v", err)
	}
	t.Cleanup(func() { UnloadFTSLibrary() })

	testDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	db = testDB
	t.Cleanup(func() { testDB.Close() })

	testWriteDB, err := initWriteDB(dbPath)
	if err != nil {
		t.Fatalf("initWriteDB failed: %v", err)
	}
	writeDB = testWriteDB
	t.Cleanup(func() { testWriteDB.Close() })

	engine, err := NewFTS(dbPath, 5000, false)
	if err != nil {
		t.Fatalf("NewFTS failed: %v", err)
	}
	fts = engine
	t.Cleanup(func() { engine.Close() })

	queryExecutor = NewQueryExecutor(db, fts)
}

func callHandler(t *testing.T, handler http.HandlerFunc, payload interface{}) (int, []byte) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func queryIDs(t *testing.T, payload map[string]interface{}) []string {
	t.Helper()
	code, body := callHandler(t, handleQuery, payload)
	if code != http.StatusOK {
		t.Fatalf("query returned HTTP %d: %s", code, body)
	}
	var records []map[string]interface{}
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("failed to parse response: %v (%s)", err, body)
	}
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = fmt.Sprintf("%v", record["id"])
	}
	return ids
}

const (
	integrationDocs = 3000
	// halfTerm 出現在半數文件，用來確認搜尋不再被 ftscore 的預設 20 筆截斷。
	halfTerm = "半數標記"
	// tenthTerm 出現在十分之一的文件。
	tenthTerm = "十一標記"
)

func seedIntegrationCorpus(t *testing.T) {
	t.Helper()

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "docs",
		"primary_key": "id",
		"fts":         map[string]interface{}{"stemming": false},
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "title", "type": "text", "searchable": true},
			{"name": "body", "type": "text", "searchable": true},
			{"name": "status", "type": "text"},
			{"name": "created_at", "type": "integer"},
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	statuses := []string{"done", "draft", "review"}
	for i := 0; i < integrationDocs; i++ {
		text := "內容 填充"
		if i%2 == 0 {
			text += " " + halfTerm
		}
		if i%10 == 0 {
			text += " " + tenthTerm
		}
		code, body := callHandler(t, handleDocumentUpsert, map[string]interface{}{
			"collection": "docs",
			"document": map[string]interface{}{
				"id":         fmt.Sprintf("d%06d", i),
				"title":      "標題",
				"body":       text,
				"status":     statuses[i%3],
				"created_at": 1700000000 + i,
			},
		})
		if code < 200 || code > 299 {
			t.Fatalf("upsert %d returned HTTP %d: %s", i, code, body)
		}
	}
}

// TestSearchReturnsEveryMatch 確認全文搜尋回傳所有命中的文件。
//
// pocket-fts 曾經不傳 limit 給 ftscore，ftscore 就套用它自己的預設值 20，
// 所以每次搜尋只拿得到 20 筆候選。回傳筆數看起來合理，內容卻少了大半。
func TestSearchReturnsEveryMatch(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	got := queryIDs(t, map[string]interface{}{
		"collection": "docs",
		"search":     map[string]string{"term": halfTerm},
		"limit":      integrationDocs,
	})
	if len(got) != integrationDocs/2 {
		t.Fatalf("got %d matches, want %d", len(got), integrationDocs/2)
	}
}

// TestSearchWithFilterIsComplete 確認 SQL 篩選作用在全部命中之上，
// 而不是只作用在前幾筆候選。
func TestSearchWithFilterIsComplete(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	got := queryIDs(t, map[string]interface{}{
		"collection": "docs",
		"search":     map[string]string{"term": halfTerm},
		"sql":        [][]interface{}{{"status", "=", "done"}},
		"limit":      integrationDocs,
	})

	want := 0
	for i := 0; i < integrationDocs; i++ {
		if i%2 == 0 && i%3 == 0 {
			want++
		}
	}
	if len(got) != want {
		t.Fatalf("got %d matches, want %d", len(got), want)
	}
}

// TestSearchOrderByColumnSeesEveryMatch 確認依欄位排序時，排序的對象是全部
// 命中的文件。截斷候選會讓「最新的一筆」這種問題安靜地回錯答案。
func TestSearchOrderByColumnSeesEveryMatch(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	got := queryIDs(t, map[string]interface{}{
		"collection": "docs",
		"search":     map[string]string{"term": tenthTerm},
		"limit":      1,
		"order_by":   []map[string]string{{"field": "created_at", "direction": "desc"}},
	})
	if len(got) != 1 {
		t.Fatalf("got %d records, want 1", len(got))
	}
	// 十分之一標記落在 i%10 == 0，最大的那筆是 2990。
	want := fmt.Sprintf("d%06d", 2990)
	if got[0] != want {
		t.Fatalf("newest matching document is %q, want %q", got[0], want)
	}
}

// TestSearchPaginationBeyondFirstPage 確認搜尋結果可以翻到第一頁之後。
func TestSearchPaginationBeyondFirstPage(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	page1 := queryIDs(t, map[string]interface{}{
		"collection": "docs", "search": map[string]string{"term": tenthTerm},
		"limit": 20, "offset": 0,
		"order_by": []map[string]string{{"field": "created_at", "direction": "asc"}},
	})
	page2 := queryIDs(t, map[string]interface{}{
		"collection": "docs", "search": map[string]string{"term": tenthTerm},
		"limit": 20, "offset": 20,
		"order_by": []map[string]string{{"field": "created_at", "direction": "asc"}},
	})

	if len(page1) != 20 || len(page2) != 20 {
		t.Fatalf("page sizes are %d and %d, want 20 and 20", len(page1), len(page2))
	}
	if page1[0] != "d000000" || page2[0] != "d000200" {
		t.Fatalf("pages start at %q and %q, want d000000 and d000200", page1[0], page2[0])
	}
}

// TestQueryOrderByIsApplied 確認 order_by 真的排序，且 asc 與 desc 相反。
func TestQueryOrderByIsApplied(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	desc := queryIDs(t, map[string]interface{}{
		"collection": "docs", "limit": 5,
		"sql":      [][]interface{}{{"created_at", ">=", 1700000000}},
		"order_by": []map[string]string{{"field": "created_at", "direction": "desc"}},
	})
	asc := queryIDs(t, map[string]interface{}{
		"collection": "docs", "limit": 5,
		"sql":      [][]interface{}{{"created_at", ">=", 1700000000}},
		"order_by": []map[string]string{{"field": "created_at", "direction": "asc"}},
	})

	if desc[0] != "d002999" {
		t.Fatalf("desc starts at %q, want d002999", desc[0])
	}
	if asc[0] != "d000000" {
		t.Fatalf("asc starts at %q, want d000000", asc[0])
	}
}

// TestQueryRejectsUnknownOrderByField 確認欄位寫錯會回 400，不再靜默忽略。
func TestQueryRejectsUnknownOrderByField(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs", "limit": 3,
		"order_by": []map[string]string{{"field": "no_such_column", "direction": "desc"}},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("got HTTP %d (%s), want 400", code, body)
	}
}

// TestNotQueryKeepsRowsWithoutTheField 確認 $not 的語意：條件算不出真假的列
// 要被保留。編譯成 SQL 時若直接寫 NOT (...)，這些列會被一併排除。
func TestNotQueryKeepsRowsWithoutTheField(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	if _, err := db.Exec("UPDATE docs SET status = NULL WHERE id = 'd000001'"); err != nil {
		t.Fatalf("failed to null out a status: %v", err)
	}

	got := queryIDs(t, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"$not": map[string]interface{}{"sql": map[string]interface{}{"where": map[string]interface{}{"status": "done"}}}},
		"result":     map[string]interface{}{"limit": integrationDocs},
	})

	sort.Strings(got)
	found := sort.SearchStrings(got, "d000001") < len(got) && got[sort.SearchStrings(got, "d000001")] == "d000001"
	if !found {
		t.Fatal("a row whose status is NULL was excluded by $not; it should be kept")
	}
}
