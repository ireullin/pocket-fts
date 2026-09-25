# 05 — 新增 `/documents/update` 與 `Store.Update`：條件式部分更新

**What to build:** 一個「只更新已存在文件」的寫入動作，可選擇帶條件。條件成立才寫入，
不成立回 409 並附上目前的文件。用來解決呼叫端「先讀再寫」的競態。

來源：paper_workers 提出的需求（`.scratch/conditional-write-requirement.md`，
paper-workers#103）：兩個 meeting-summary-worker 讀-改-寫同一筆會議，8 場裡 3 場被後寫
的蓋掉。2026-09-25 grilling 改了原提案：不在 upsert 上加 `if`，改成新增 update。
paper_workers 維持舊版，升級時才改用。

## 已定案的設計

- `/documents/upsert` 不變：沒有就寫入，有就覆寫。
- `/documents/update`：
  - 只改請求帶的欄位（等同 `UPDATE ... SET`），沒帶的欄位維持原值。主鍵必帶，不能改。
  - `where` 選填，格式同 `/query` 扁平格式的 `sql`：`[欄位, 運算子, 值]` 陣列，AND 串接，
    運算子 `=`、`!=`、`>`、`>=`、`<`、`<=`、`LIKE`。欄位對照 schema 驗證，寫錯回 400。
  - 文件不存在 → 404。
  - 條件不成立 → 409，body 帶錯誤訊息、不成立的條件、文件目前的完整內容。
  - 成功 → 200。
- 在 [04](./write-lock.md) 的寫入鎖內：讀原列 → 比對條件 → 合併欄位 → 照 04 的順序寫
  SQL 與 ftscore。ftscore 收到合併後的完整文件，改到 searchable 欄位時重新切詞，舊詞
  由 v0.14 修好的 trigger 移除。

**Blocked by:** [04](./write-lock.md)

**Status:** ready-for-agent

## 驗收條件

- [ ] 條件成立：200，只有帶的欄位改變。
- [ ] 條件不成立：409，資料未改變，body 帶目前的文件。
- [ ] 文件不存在：404，沒有建立文件。
- [ ] `where` 引用不存在的欄位：400。
- [ ] 改 searchable 欄位後，新詞搜得到、舊詞搜不到。
- [ ] 併發測試：N 個請求同時帶 `where adopted = ""` 更新同一份文件，恰好 1 個 200，其餘 409。
- [ ] `Store.Update` 提供同樣語意。
- [ ] API_REFERENCE 新增 `/documents/update` 一節（不提函式庫）。
- [ ] 真實服務 e2e：重現 paper_workers 的兩個 worker 情境，後寫的一方收到 409。
