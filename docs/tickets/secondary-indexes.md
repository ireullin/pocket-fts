# 02 — db.sqlite 次要索引（`indexes` schema 陣列）

**What to build:** 建立 collection 時可以在 schema 頂層傳 `"indexes":
[["field1"], ["field2", "created_at"]]`——欄位名稱陣列的陣列，每一項對應一個
`CREATE INDEX`，一個元素是單欄索引、多個元素是複合索引（欄位順序照宣告順序，因為順序
會影響 SQLite 的查詢計畫，由呼叫端自己決定）。這個機制跟 `searchable`（全文索引開關，
見 [01](./searchable-rename.md)）完全正交、互不影響：一個欄位要不要進 `db.sqlite`
索引，只看它有沒有被 `indexes` 陣列引用，跟它是不是全文搜尋欄位無關。

`indexes` 陣列只在建立當下生效。schema 目前沒有事後修改的 API（只有
`create`／`delete`／`list`／`content` 四個端點），這次也不新增——既有 collection 想補
索引，一樣要刪掉重建，跟目前唯一改 schema 的方式一致。

**Blocked by:** None — can start immediately（不依賴
[01](./searchable-rename.md)，`indexes` 的語意不涉及全文索引欄位叫什麼名字）

**Status:** ready-for-agent

- [ ] `CollectionSchema` 新增 `Indexes [][]string`，JSON key `indexes`，
      `omitempty`。
- [ ] `generateCreateTableSQL` 依 `schema.Indexes` 額外產生對應的 `CREATE INDEX`
      陳述式（跟現有的 `CREATE TABLE` 一起執行，或緊接在後面依序執行）。
- [ ] 驗證：`indexes` 裡任何一項引用到 schema 沒有定義的欄位名稱，
      `/collections/create` 回 400，不建立任何東西（含已經成功的 FTS collection
      也要 rollback，維持原子性；若做不到完全 rollback，至少要在驗證階段就攔下來，
      不要建到一半才發現）。
- [ ] 驗證：`indexes` 的每一項至少要有 1 個欄位名稱，空陣列項目回 400。
- [ ] 建立成功後，查詢 `sqlite_master` 確認索引真的被建出來，欄位順序跟宣告順序一致
      （複合索引的欄位順序決定查詢計畫，測試要能證明順序沒被打亂）。
- [ ] 重複宣告已經有索引的欄位（例如同時是 primary key、又被列進 `indexes`）不報錯，
      SQLite 的 `CREATE INDEX IF NOT EXISTS` 或等效邏輯處理掉重複建立的情況。
- [ ] `API_REFERENCE.md` 的 Create Collection 一節新增 `indexes` 欄位說明與範例。
