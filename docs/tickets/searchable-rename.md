# 01 — `indexed` 改名 `searchable`，修正送給 ftscore 的 payload

**What to build:** 呼叫端用 `"searchable": true`（不再是 `"indexed"`）宣告一個欄位要進全文
索引。`/collections/create` 收到請求後，不再把 request body 原封不動轉送給 ftscore，
改成組一份把 `searchable` 翻譯回 `indexed` 這個 key 名稱的 JSON 再呼叫
`fts.CreateCollection`——因為 ftscore 自己的 schema 解析器認的欄位名稱是 `indexed`，這是
既有、沒被這次改動觸碰的行為。FTS 的建立、寫入、搜尋行為對呼叫端而言跟改名前完全一致，
只有 schema 裡的 key 名稱變了。

這是對外 API 的 breaking change：既有呼叫端若還在送 `"indexed": true`，改名後不會再被
識別為需要全文索引，等同建出一個沒有 FTS 的 collection。決定接受這個 breaking change，
不做新舊欄位名稱相容。

**Blocked by:** None — can start immediately

**Status:** closed（2026-09-25）

- [x] `Field` struct 的 JSON tag 從 `indexed` 改成 `searchable`；程式碼裡對應的識別字
      （`Indexed` → `Searchable`）一併改名。
- [x] `schemaHasFTS` 改看 `field.Searchable`。
- [x] `handleCollectionCreate` 不再把 request body 原封不動傳給 `fts.CreateCollection`；
      改成用解析後的 schema 組一份 ftscore 專用的 JSON，把每個欄位的 `searchable` 值寫進
      `indexed` 這個 key。
- [x] 既有測試（`ftscore_scope_test.go` 等）用到 `"indexed": true` 的地方全部改成
      `"searchable": true`，且測試維持全部通過——證明改名後 FTS 行為跟改名前一致。
- [x] 新增測試：確認建立 collection 後，ftscore 那邊實際收到的 schema 裡欄位名稱是
      `indexed`（不是 `searchable`），full-text search 能正確搜到內容。
- [x] `API_REFERENCE.md` 的 Field object 表格、範例 JSON 全部從 `indexed` 改成
      `searchable`，並註明這是 breaking change。
- [x] `docs/tickets/ftscore-scope-separation.md` 裡引用 `indexed` 的地方（規則說明、
      `schemaHasFTS` 描述）同步改成 `searchable`，避免文件跟程式碼不一致。

## 關票核對（2026-09-25）

實作於 `02f6ea6`。逐條核對：

1. `Field.Searchable`，JSON tag `searchable`——`src/handlers.go:21`。`Indexed` 只剩 ftscore 專用的 `ftsField`（`handlers.go:967`）。層級：程式碼檢視。
2. `schemaHasFTS` 看 `field.Searchable`——`handlers.go:952`。層級：程式碼檢視 + `TestNonFTSCollectionNeverTouchesFtscore` 通過。
3. `handleCollectionCreate` 呼叫 `schemaForFTS` 組 payload，不轉送 request body——`handlers.go:303`。層級：程式碼檢視。
4. 測試裡已無 `"indexed": true`（grep `src/*_test.go` 只剩 `schemaForFTS` 測試裡刻意解析 ftscore payload 的 struct tag）；`go test ./src/` 全部通過。層級：單元／整合測試。
5. `TestSchemaForFTSTranslatesSearchableToIndexed` 驗 payload 只有 `indexed`；`TestFTSCollectionStillReachesFtscore` 建 collection 後直接呼叫 `fts.Search` 搜得到。另外用 `bin/pocket_fts` 起真實服務，`"searchable": true` 建 collection、寫 60 筆，`/query` 搜 `apple` 回 30 筆。層級：e2e。
6. `API_REFERENCE.md` Field object 表格用 `searchable`，附 breaking change 說明。層級：文件檢視。
7. `ftscore-scope-separation.md` 只剩「原名 `indexed`」的說明文字。層級：文件檢視。
