package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// result.fields 過去只檢查識別字格式，不對照 collection schema。欄位名稱寫錯
// 就被串進 SELECT，由 SQLite 回報錯誤，呼叫端拿到 HTTP 500 與一段 SQL 錯誤
// 訊息，看不出是自己弄錯。以下測試把行為釘成與 order_by 一致的 400。

// TestQueryRejectsUnknownResultField 確認 result.fields 寫錯會回 400。
func TestQueryRejectsUnknownResultField(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"sql": map[string]interface{}{"where": map[string]interface{}{"status": "done"}}},
		"result":     map[string]interface{}{"fields": []string{"id", "no_such_column"}, "limit": 3},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("got HTTP %d (%s), want 400", code, body)
	}
}

// TestQueryAcceptsScoreInResultFields 確認 _score 可以被選取，規則比照 order_by：
// 帶 search 子句才產生分數。
func TestQueryAcceptsScoreInResultFields(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"search": map[string]interface{}{"term": tenthTerm}},
		"result":     map[string]interface{}{"fields": []string{"id", "_score"}, "limit": 3},
	})
	if code != http.StatusOK {
		t.Fatalf("got HTTP %d (%s), want 200", code, body)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("failed to parse response: %v (%s)", err, body)
	}
	if len(records) == 0 {
		t.Fatal("expected at least one record")
	}
	for _, record := range records {
		if _, ok := record["id"]; !ok {
			t.Fatalf("expected id in %v", record)
		}
		if _, ok := record["_score"]; !ok {
			t.Fatalf("expected _score in %v", record)
		}
	}
}

// TestQueryRejectsScoreWithoutSearch 確認沒有 search 子句時選 _score 會回 400，
// 與 order_by 引用 _score 卻沒有 search 子句的處理一致。
func TestQueryRejectsScoreWithoutSearch(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"sql": map[string]interface{}{"where": map[string]interface{}{"status": "done"}}},
		"result":     map[string]interface{}{"fields": []string{"id", "_score"}, "limit": 3},
	})
	if code != http.StatusBadRequest {
		t.Fatalf("got HTTP %d (%s), want 400", code, body)
	}
}

// TestQueryReturnsOnlySelectedResultFields 確認驗證沒有擋掉正確的請求。
func TestQueryReturnsOnlySelectedResultFields(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"sql": map[string]interface{}{"where": map[string]interface{}{"status": "done"}}},
		"result":     map[string]interface{}{"fields": []string{"id", "status"}, "limit": 2},
	})
	if code != http.StatusOK {
		t.Fatalf("got HTTP %d (%s), want 200", code, body)
	}

	var records []map[string]interface{}
	if err := json.Unmarshal(body, &records); err != nil {
		t.Fatalf("failed to parse response: %v (%s)", err, body)
	}
	if len(records) != 2 {
		t.Fatalf("got %d records, want 2", len(records))
	}
	for _, record := range records {
		if len(record) != 2 {
			t.Fatalf("expected only the two selected fields, got %v", record)
		}
		for _, want := range []string{"id", "status"} {
			if _, ok := record[want]; !ok {
				t.Fatalf("expected %q in %v", want, record)
			}
		}
	}
}

// TestQueryAcceptsDifferentlyCasedResultFields 確認只是大小寫不同的欄位名稱
// 仍然回 200。SQLite 的欄位名稱不分大小寫，這個請求在加入驗證之前就能用。
func TestQueryAcceptsDifferentlyCasedResultFields(t *testing.T) {
	setupQueryEngine(t)
	seedIntegrationCorpus(t)

	code, body := callHandler(t, handleQuery, map[string]interface{}{
		"collection": "docs",
		"query":      map[string]interface{}{"sql": map[string]interface{}{"where": map[string]interface{}{"status": "done"}}},
		"result":     map[string]interface{}{"fields": []string{"ID", "Status"}, "limit": 1},
	})
	if code != http.StatusOK {
		t.Fatalf("got HTTP %d (%s), want 200", code, body)
	}
}
