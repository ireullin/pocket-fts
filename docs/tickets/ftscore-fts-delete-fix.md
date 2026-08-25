# ftscore FTS 索引刪除缺陷修復：pocket-fts 因應

`src/embedded/ftscore` 換成 ftscore `v0.14`。這一版修正了一個會讓 FTS5 索引持續累積
錯誤的缺陷。本 ticket 記錄 pocket-fts 這側的因應，缺陷本身的完整說明與既有 `.indices`
的修復程序見 ftscore README 第 10 節。

## 背景：缺陷是什麼

ftscore 的 FTS5 索引是外部內容表（`content=`），只存倒排索引，不存原文。一列被改或被
刪時，FTS5 無法自己知道舊文字是什麼，所以 SQLite 規定呼叫端要在 `'delete'` 指令裡把
舊值交回去。

ftscore 的 AFTER UPDATE 與 AFTER DELETE trigger 原本只帶 `old.rowid`。省略的欄位是
NULL，FTS5 對 NULL 斷出零個詞，一個索引項目都沒扣掉——但它仍然刪掉 `_docsize` 那一列，
並把 averages 記錄裡的 `nRow` 減一。

扣統計，不扣索引。當某個詞的索引項目數超過 `nRow`，BM25 的 IDF 對負數取對數而得到
NaN，SQLite 把 NaN 表示成 NULL。

## pocket-fts 這側看到的樣子

`src/query.go:236` 的 `executeSearchQuery` 把 ftscore 的錯誤包起來往上傳，最後成為 500：

```
Query execution failed: failed to execute query: FTS search failed: scan search row:
sql: Scan error on column index 2, name "score": converting NULL to float64 is unsupported
```

`Query execution failed` 與 `FTS search failed` 是 pocket-fts 加的，`scan search row`
之後是 ftscore 的。pocket-fts 的程式碼沒有缺陷，但錯誤是 pocket-fts 送出去的。

另外兩個症狀更早就開始了，只是看不見：改過的文件仍以舊內容被搜到，刪掉的文件仍然
出現在搜尋結果。

## 這次做的變更

### 1. 更新嵌入的 ftscore（v0.13 → v0.14）

`src/embedded/ftscore` 換成新建的 `libftscore.so`。

C 介面沒有新增或移除符號，`src/fts_dl.go` 的 dlsym 清單不需要調整。

ftscore 這一版包含兩項改動：刪除 trigger 帶上 `old` 各欄位值（修正根因），以及搜尋分數
改掃進 `sql.NullFloat64`（金絲雀——索引若再度損壞，搜尋降級為 0 分並記一則 Warn，而不是
整次失敗）。

### 2. 重建 `bin/pocket_fts`

`CGO_ENABLED=1 go build -o bin/pocket_fts ./src`，與嵌入的函式庫一起提交。

### 3. 新增整合測試 `src/fts_index_sync_test.go`

既有的整合測試接真實的嵌入式二進位檔，但沒有任何一個涵蓋「更新或刪除之後再搜尋」。
換句話說，這個缺陷可以在測試全綠的情況下存在。補兩個測試把契約釘住：

- `TestUpdatedDocumentDropsOldTerms`：改過的文件不再以舊內容被搜到。
- `TestDeletedDocumentLeavesNoIndexResidue`：反覆改寫並刪除之後，查詢仍回 200，命中數
  等於剩餘文件數，且改寫前的內容已從索引移除。

兩個測試都在舊版函式庫下失敗。第二個測試在舊版下拿到的正是上面那段 500 回應。

## 測試

`go test ./...` 全部通過。

## 既有 `.indices` 的處置

**不做自動遷移。** `CREATE TRIGGER IF NOT EXISTS` 不會覆蓋既有 trigger，換掉 trigger 也
不會清除已經累積的殘留項目，所以升級這個執行檔並不會修好既有的 `.indices` 檔案。

pocket-fts 的部署模型是每次部署都重建（見 `ftscore-scope-separation.md`），新建的
collection 直接拿到正確的 trigger，不需要任何動作。

若某個部署確實要沿用既有的 `.indices`，依 ftscore README 第 10 節的四個步驟原地修復：
偵測舊 trigger、抄 `_ai` trigger 的欄位清單、`DROP` 後重建兩個 trigger、對每個 collection
下 `'rebuild'`。該程序已在一份真實損壞的 `.indices` 副本上驗證過。

## 不適用

- 不新增自動偵測或自動遷移機制。
- 不調整 `src/fts_dl.go` 的 dlsym 清單（沒有新符號）。
- 下游專案自己的緩解措施與資料庫不在本次範圍內。
