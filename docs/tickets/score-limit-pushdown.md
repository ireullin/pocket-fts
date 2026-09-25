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

**Status:** closed（2026-09-25）

## 驗收條件

- [x] 修改前先記錄 pocket-fts-bench「全文搜尋 + SQL 篩選」與「依 `_score` 排序 + 分頁」
      的 rps，修改後同條件再量一次，兩組數字寫進這張票。
- [x] 既有排序測試（`ordering_test`、`query_integration_test`）全部通過。
- [x] 差異測試：多組「搜尋 + 篩選 + `_score` 排序 + 分頁」的結果與修改前一致。
- [x] `$or` 混合查詢：沒有分數的紀錄仍不帶 `_score`、排在最後。
- [x] 真實服務 e2e：同上幾組查詢。

## 關票核對（2026-09-25）

實作於 `1cb593c`。**設計偏離，使用者已裁定接受**：票上定案是修法 B（`json_each` JOIN），原型實測比修改前慢（併發 1：15.5 → 12.1 rps，SQLite 要替 1 萬筆分數建暫存表與自動索引）。改用註冊的 SQL 函式 `pfts_score(handle, pk)` 從 Go 的分數表查分數，ORDER BY 與 LIMIT/OFFSET 仍交給 SQLite、回傳格式不變。2026-09-25 使用者選「函式做法」。

逐條核對：

1. 量測（10 萬筆語料，pocket-fts-bench 的 R6 形狀：搜尋「常見標記」+ `status = done` + limit 20；另一組加 `_score desc` 與 offset 40；服務綁 0-7 核、壓測綁 8-23 核，閉迴路 10 秒）：

   | 情境 | 修改前 | 修改後 |
   |---|---|---|
   | 預設排序，併發 1 | 15.5 rps（p50 64.1ms） | 15.8 rps（p50 62.9ms） |
   | 預設排序，併發 8 | 25.4 rps | 25.2 rps |
   | `_score desc` + offset 40，併發 1 | 15.5 rps | 15.8 rps |
   | `_score desc` + offset 40，併發 8 | 25.7 rps | 25.2 rps |

   CPU profile：`fetchRecords` 每次查詢 14ms → 12ms；ftscore 搜尋 1 萬筆命中約佔 43ms，是主要成本。層級：實測。
2. 既有排序測試：`ordering_test` 裡驗 Go 排序函式的 8 個測試（`TestSortRecords*`、`TestCompareValues*`、`TestApplyLimitOffset`）隨函式一起刪除，語意改由 `score_order_test.go` 的 6 個測試對 SQL 路徑驗證（desc／asc、預設、分頁、沒有分數的列、同分）；其餘排序測試與 `query_integration_test` 全部通過。這一條的字面「全部通過」沒有照做，使用者裁定接受函式做法時一併接受。層級：整合測試。
3. 差異測試：修改前後對 7 組查詢（搜尋 + 篩選 + `_score` 排序 + 分頁、多鍵、`$or`、`$and` + `$not`、欄位子集）的回應比對，6 組逐位元組相同；1 組 `$or`（limit 200，133 筆）集合相同、有分數的列順序相同，只有沒有分數的列彼此的順序由查詢計畫決定的順序（3、9、12、15、18、6…）改成主鍵升冪。層級：e2e（repo 外）。
4. `$or` 混合查詢：`TestUnscoredRowsCountAsLeastRelevant` 驗證沒有分數的列不帶 `_score`，desc 時排最後、asc 時排最前（與修改前一致）。層級：整合測試。
5. 真實服務 e2e：同第 3 條，另外 49 個請求的比對與修改前相同（見 03、04）。層級：e2e。
