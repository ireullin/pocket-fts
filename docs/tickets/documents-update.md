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

**Status:** closed（2026-09-25）

## 驗收條件

- [x] 條件成立：200，只有帶的欄位改變。
- [x] 條件不成立：409，資料未改變，body 帶目前的文件。
- [x] 文件不存在：404，沒有建立文件。
- [x] `where` 引用不存在的欄位：400。
- [x] 改 searchable 欄位後，新詞搜得到、舊詞搜不到。
- [x] 併發測試：N 個請求同時帶 `where adopted = ""` 更新同一份文件，恰好 1 個 200，其餘 409。
- [x] `Store.Update` 提供同樣語意。
- [x] API_REFERENCE 新增 `/documents/update` 一節（不提函式庫）。
- [x] 真實服務 e2e：重現 paper_workers 的兩個 worker 情境，後寫的一方收到 409。

## 關票核對（2026-09-25）

實作於 `fcc4630`，審查修正 `eb462c2`。逐條核對：

1. 條件成立 200、只改帶的欄位：`TestUpdateChangesOnlyGivenFieldsWhenConditionHolds`、`TestUpdateEndpointAppliesWhenConditionHolds`。層級：整合測試。
2. 條件不成立 409、資料不變、body 帶目前文件：`TestUpdateConflictLeavesRowAndReportsCurrent`、`TestUpdateEndpointConflictReturns409WithCurrentDocument`。層級：整合測試。
3. 文件不存在 404、不建立：`TestUpdateMissingDocumentIsNotFound`（有無條件各一次）、`TestUpdateEndpointStatusCodes`。層級：整合測試。
4. `where` 欄位不存在 400：同上兩個測試，另含未知運算子、格式錯誤、document 未知欄位、缺主鍵。層級：整合測試。
5. 改 searchable 欄位後新詞搜得到、舊詞搜不到：`TestUpdateOfSearchableFieldReindexes`。層級：整合測試（真實 ftscore）。
6. 併發：`TestConcurrentConditionalUpdatesAdmitOne`，8 個 goroutine 帶 `adopted = ""`，恰好 1 個成功、7 個 409。層級：整合測試。
7. `Store.Update` 是 HTTP 端點的實作本體，語意相同。層級：程式碼檢視。
8. API_REFERENCE 新增 Update Document 一節（200／409／404／400／503），Write Concurrency 一節補上 update。沒有提到函式庫。層級：文件檢視。
9. 真實服務 e2e：重現 paper_workers 兩個 worker（兩邊都先讀到 `adopted_version_id = ""`），8 場都是先寫的 gemini 200、後寫的 qwen 409 並在 body 看到 gemini，最終採用 gemini；8 個併發請求得到 1 個 200 與 7 個 409；不存在的文件回 404 且沒有被建立。容器實測也得到 200 → 409。層級：e2e。
