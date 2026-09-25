# 04 — 寫入加全域鎖，SQL 交易包住 ftscore 寫入

**What to build:** 讓一次 upsert／delete 的「寫 ftscore + 寫 SQL」成為不會交錯、一起
成功或一起失敗的動作。

## 缺陷（2026-09-25 讀程式碼發現，未實測重現）

`handleDocumentUpsert` 先 `fts.UpsertDocument` 再 `execWrite`（`src/handlers.go:681-705`），
`handleDocumentDelete` 先 `fts.DeleteDocument` 再 `execWrite`（`handlers.go:757-772`）。
兩步之間沒有鎖（`src/` 沒有任何 mutex），也不在同一個交易：

- 兩個請求同時更新同一份文件，順序可以是「A 寫 ftscore → B 寫 ftscore → B 寫 SQL →
  A 寫 SQL」，結果全文索引存 B、資料表存 A。
- SQL 寫入失敗時，ftscore 已寫入，不會回滾。

## 已定案的設計

- 一把全域寫入鎖，範圍涵蓋整個 upsert、update、delete。兩個資料庫本來就只有一個寫入者，
  吞吐量上限不變。沒有 FTS 的 collection 也排這個隊。
- 順序改成：取鎖 → 開 SQL 交易並寫入（不 commit）→ 寫 ftscore → ftscore 失敗就
  rollback，成功就 commit → 放鎖。只剩「ftscore 成功、SQL commit 失敗」一種不一致。
- 寫入逾時（`-write-timeout`）涵蓋等鎖的時間，逾時照舊回 503。

**Blocked by:** [03](./store-library.md)

**Status:** closed（2026-09-25）

## 驗收條件

- [x] 先寫一個測試，在修改前重現交錯（兩個 goroutine 對同一份文件寫不同內容，
      結束後全文索引與資料表內容不一致），確認它在修改前失敗。
- [x] 修改後該測試通過。
- [x] 測試：ftscore 寫入失敗時，SQL 資料表維持原值。
- [x] 測試：等鎖超過寫入逾時回 503 加 `Retry-After`。
- [x] 既有測試全部通過。
- [x] 真實服務 e2e：併發寫同一份文件後，`/query` 搜尋結果與資料表內容一致。

## 關票核對（2026-09-25）

實作於 `1aa5d5d`，審查修正 `eb462c2`。逐條核對：

1. 修改前重現：`TestConcurrentUpsertsKeepIndexAndTableInSync` 用 `betweenWrites` hook 讓寫入 A 停在兩步之間，修改前失敗，訊息是「index and table disagree: the table holds "甲標記" but the index finds "乙標記"」。層級：單元測試。
2. 修改後同一個測試通過。層級：單元測試。
3. `TestFailedFTSWriteLeavesTableUnchanged`：ftscore 拒絕（searchable 欄位給數字）時，既有列維持「舊內容」，新列不會被插入。層級：整合測試（真實 ftscore）。
4. 503：`TestWriteWaitingForLockTimesOut` 驗證等鎖超過 200ms 寫入逾時回 `ErrWriteTimeout`（upsert 與 delete）；`TestWriteTimeoutMapsTo503` 驗證該錯誤在 HTTP 層回 503 加 `Retry-After`。兩段分開驗，沒有單一測試走完整條 HTTP 路徑。審查後 collection 建立／刪除等鎖逾時也改回 503。層級：單元測試。
5. 既有測試全部通過。層級：測試。
6. 真實服務 e2e：16 執行緒對 4 份文件併發 upsert 共 960 次，之後逐份比對資料表與 `/query` 搜尋，4 份一致。舊版同一個腳本也是一致，但有 2 次 500（ftscore 內部 `UNIQUE constraint failed: docs.id (1555)`），新版 0 次。層級：e2e。

副作用（已知、未修）：upsert 帶 schema 沒有的欄位時，原本由 ftscore 先拒絕（500「Failed to upsert document: missing required field title」），現在由 SQL 先拒絕（500「Failed to save document to SQL table」）。狀態碼相同，訊息不同。
