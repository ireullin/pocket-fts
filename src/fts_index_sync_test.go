package main

import (
	"fmt"
	"net/http"
	"testing"
)

// ftscore 的 FTS 索引是外部內容表，只有在刪除 trigger 把舊值交回 fts5 時，
// 索引才跟得上主表。修復前的 trigger 只帶 rowid，於是改過的舊內容與刪掉的
// 資料永遠搜得到，索引持續累積殘留，最後讓 bm25 回傳 NULL、搜尋回 500。
//
// 這兩個測試接真實的嵌入式 ftscore 二進位檔，把那個契約釘在 pocket-fts 這一側：
// 每次替換 src/embedded/ftscore 都會重驗一次。

func seedSyncCollection(t *testing.T) {
	t.Helper()
	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "sync_docs",
		"primary_key": "id",
		"fts":         map[string]interface{}{"stemming": false},
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "body", "type": "text", "searchable": true},
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}
}

func upsertSyncDoc(t *testing.T, id, body string) {
	t.Helper()
	code, resp := callHandler(t, handleDocumentUpsert, map[string]interface{}{
		"collection": "sync_docs",
		"document":   map[string]interface{}{"id": id, "body": body},
	})
	if code < 200 || code > 299 {
		t.Fatalf("upsert %s returned HTTP %d: %s", id, code, resp)
	}
}

func searchSyncIDs(t *testing.T, term string) []string {
	t.Helper()
	return queryIDs(t, map[string]interface{}{
		"collection": "sync_docs",
		"search":     map[string]string{"term": term},
		"limit":      100,
	})
}

// TestUpdatedDocumentDropsOldTerms 確認改過的文件不再以舊內容被搜到。
func TestUpdatedDocumentDropsOldTerms(t *testing.T) {
	setupQueryEngine(t)
	seedSyncCollection(t)

	upsertSyncDoc(t, "s1", "起始 內容 甲標記")
	if got := searchSyncIDs(t, "甲標記"); len(got) != 1 {
		t.Fatalf("更新前搜尋舊內容應命中 1 筆，得到 %d 筆", len(got))
	}

	upsertSyncDoc(t, "s1", "修改後 內容 乙標記")

	if got := searchSyncIDs(t, "甲標記"); len(got) != 0 {
		t.Fatalf("更新後搜尋舊內容應命中 0 筆，得到 %d 筆：%v", len(got), got)
	}
	if got := searchSyncIDs(t, "乙標記"); len(got) != 1 {
		t.Fatalf("更新後搜尋新內容應命中 1 筆，得到 %d 筆", len(got))
	}
}

// TestDeletedDocumentLeavesNoIndexResidue 確認刪掉的文件不再被搜到，
// 且反覆改寫之後搜尋仍然成功——修復前這裡會回 500。
func TestDeletedDocumentLeavesNoIndexResidue(t *testing.T) {
	setupQueryEngine(t)
	seedSyncCollection(t)

	const total = 6
	for i := 1; i <= total; i++ {
		upsertSyncDoc(t, fmt.Sprintf("s%d", i), "共用 標記 內容")
	}
	for round := 1; round <= 4; round++ {
		for i := 1; i <= total; i++ {
			upsertSyncDoc(t, fmt.Sprintf("s%d", i), fmt.Sprintf("共用 標記 改寫 %d", round))
		}
	}

	code, body := callHandler(t, handleDocumentDelete, map[string]interface{}{
		"collection": "sync_docs",
		"id":         fmt.Sprintf("s%d", total),
	})
	if code < 200 || code > 299 {
		t.Fatalf("delete returned HTTP %d: %s", code, body)
	}

	code, body = callHandler(t, handleQuery, map[string]interface{}{
		"collection": "sync_docs",
		"search":     map[string]string{"term": "共用"},
		"limit":      100,
	})
	if code != http.StatusOK {
		t.Fatalf("反覆改寫並刪除之後查詢應回 200，得到 HTTP %d：%s", code, body)
	}

	if got := searchSyncIDs(t, "共用"); len(got) != total-1 {
		t.Fatalf("刪除後應命中 %d 筆，得到 %d 筆：%v", total-1, len(got), got)
	}
	if got := searchSyncIDs(t, "內容"); len(got) != 0 {
		t.Fatalf("改寫前的內容應已從索引移除，卻命中 %d 筆：%v", len(got), got)
	}
}
