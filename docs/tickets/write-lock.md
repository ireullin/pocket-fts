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

**Status:** ready-for-agent

## 驗收條件

- [ ] 先寫一個測試，在修改前重現交錯（兩個 goroutine 對同一份文件寫不同內容，
      結束後全文索引與資料表內容不一致），確認它在修改前失敗。
- [ ] 修改後該測試通過。
- [ ] 測試：ftscore 寫入失敗時，SQL 資料表維持原值。
- [ ] 測試：等鎖超過寫入逾時回 503 加 `Retry-After`。
- [ ] 既有測試全部通過。
- [ ] 真實服務 e2e：併發寫同一份文件後，`/query` 搜尋結果與資料表內容一致。
