# 07 — 依相關性排序時把 LIMIT 下推給 SQLite

**What to build:** 全文搜尋搭配 SQL 條件、依 `_score` 排序（含不指定 `order_by` 的預設
情況）時，讓 SQLite 做排序與分頁，不再把所有命中的完整資料列讀回 Go。

## 現況

`fetchRecords`（`src/query.go:529`）在「排序用到 `_score`」或「有全文搜尋但沒指定
`order_by`」時，取回所有符合的完整資料列，在 Go 排序，再套 LIMIT。結果正確。只有
「單純全文搜尋、沒有 SQL 條件」走快速路徑 `executeRelevanceTopN`。

例：10 萬筆，`apple` 命中 1 萬，其中 `status = done` 3 千，要前 10 筆——取回並解碼
3 千筆完整資料列，丟掉 2990 筆。WORKLOG 記錄「全文搜尋 + SQL 篩選」10 萬筆語料 25 rps。

## 已定案的設計（修法 B）

- 把 ftscore 回傳的 `(id, score)` 以 `json_each` 當暫存表 JOIN 進 SQL，
  `ORDER BY score ... LIMIT ? OFFSET ?` 交給 SQLite。
- 回傳格式不變：沒有分數的紀錄（`$or`／`$not` 混合查詢才會出現）不帶 `_score` 鍵，
  排在最後。Go 讀回時遇到 NULL 分數就不放 `_score`。
- `_score` 的方向語意不變（`desc` = 最相關在前）。

**Blocked by:** [03](./store-library.md)

**Status:** ready-for-agent

## 驗收條件

- [ ] 修改前先記錄 pocket-fts-bench「全文搜尋 + SQL 篩選」與「依 `_score` 排序 + 分頁」
      的 rps，修改後同條件再量一次，兩組數字寫進這張票。
- [ ] 既有排序測試（`ordering_test`、`query_integration_test`）全部通過。
- [ ] 差異測試：多組「搜尋 + 篩選 + `_score` 排序 + 分頁」的結果與修改前一致。
- [ ] `$or` 混合查詢：沒有分數的紀錄仍不帶 `_score`、排在最後。
- [ ] 真實服務 e2e：同上幾組查詢。
