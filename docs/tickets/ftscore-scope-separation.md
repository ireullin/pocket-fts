# ftscore 職責限縮：只有需要全文搜尋的 collection 才碰 ftscore

## 核心原則（這次重新對齊的依據）

1. **ftscore 只提供全文索引功能**——它的角色是「這個 collection 裡要被全文搜尋的欄位」的
   索引器，不該是每個 collection 的必經之路。
2. **`db.sqlite` 的行為應該跟一般 SQLite 使用上沒有差別**——不管有沒有接 ftscore，這個
   SQLite 檔案本身該有的樣子（建表、索引、交易）不因為要遷就 ftscore 而變形。
3. **pocket-fts 是協調兩邊的工具**，不是「每個 collection 都得同時活在兩個資料庫裡」的
   強制規則。
4. **假設呼叫端一定經過 pocket-fts 操作。** 若繞過 pocket-fts 直接動 `db.sqlite` 或
   ftscore 的 `.indices`，導致資料不一致，能防呆就防呆；防不了的，責任在呼叫端，不是
   pocket-fts 要保證的範圍。

## 現況違反原則 1 的地方

`handlers.go` 裡有 5 個呼叫 `fts.*` 的地方，全部無條件執行，不管 schema 有沒有任何欄位
設 `searchable: true`：

| 函式 | 位置 | 呼叫 |
| --- | --- | --- |
| `handleCollectionCreate` | `handlers.go:289` | `fts.CreateCollection` |
| `handleCollectionDelete` | `handlers.go:348` | `fts.DeleteCollection` |
| `handleDocumentUpsert` | `handlers.go:624` | `fts.UpsertDocument` |
| `handleDocumentDelete` | `handlers.go:695` | `fts.DeleteDocument` |
| `handleSearch` | `handlers.go:747` | `fts.Search` |
| `executeSearchQuery`（`/query` 的 search 節點，涵蓋一般路徑與 relevance 快速路徑） | `query.go:223` | `qe.fts.Search` |

新版 ftscore 甚至要求 schema 至少有一個非 PK 的文字欄位才能 `CreateCollection` 成功，
代表現況下**根本無法建立一個純 SQL、完全不做 FTS 的 collection**——即使呼叫端只想把
pocket-fts 當一般 SQLite API 用，也被迫掛一個文字欄位餵給 ftscore。

## 判斷機制

一個 collection 要不要接 ftscore，依現有 schema 自動推斷：掃 `schema.Fields`，只要有任何
一個欄位 `searchable: true`，這個 collection 就是 FTS 啟用；一個都沒有，就完全跳過 ftscore。
不新增 schema 欄位，`searchable`（原名 `indexed`，見
[searchable-rename.md](./searchable-rename.md)）的既有語意不變。

新增共用 helper：

```go
func schemaHasFTS(schema CollectionSchema) bool {
	for _, field := range schema.Fields {
		if field.Searchable {
			return true
		}
	}
	return false
}
```

## 決定要做的程式碼變更

### 1. `handleCollectionCreate`：FTS 啟用才呼叫 `fts.CreateCollection`

Schema 已經在函式裡解析過，`schemaHasFTS(schema)` 為 false 就跳過該呼叫，直接進到儲存
schema metadata、建 SQL 表的步驟。

### 2. `handleCollectionDelete`：FTS 啟用才呼叫 `fts.DeleteCollection`

目前的刪除順序是先刪 metadata、才刪 FTS、才刪 SQL 表——刪 metadata 之後就讀不到 schema
了。這次調整順序：**先讀 schema 判斷是否 FTS 啟用，再依序刪 metadata／FTS／SQL 表**。
FTS 未啟用就跳過 `fts.DeleteCollection`，避免對一個從未在 ftscore 建過的 collection
發出刪除請求、留下誤導性的「Inconsistency」log。

### 3. `handleDocumentUpsert` / `handleDocumentDelete`：FTS 啟用才呼叫 ftscore

`handleDocumentUpsert` 目前完全沒有讀 schema（`generateUpsertSQL` 只需要 document 本身的
欄位）。這次補上 schema 讀取，判斷 `schemaHasFTS` 之後才呼叫 `fts.UpsertDocument`。
`handleDocumentDelete` 已經有讀 schema（要取 `PrimaryKey` 組 `primaryKeyJSON`），加一個
`schemaHasFTS` 判斷即可。

### 4. 搜尋防呆：collection 未啟用 FTS 時直接回清楚的 400

- `executeSearchQuery`（`query.go:189`，`/query` 的 search 節點統一入口，涵蓋一般路徑與
  relevance 快速路徑）：進入時讀 schema，`schemaHasFTS` 為 false 就回
  `newValidationError`，`handleQuery` 既有的 `ValidationError` 判斷會自動轉成 400，不需要
  改 `handlers.go`。
- `handleSearch`（舊版 `/search` 端點）：目前完全沒讀 schema，直接呼叫 `fts.Search`。這次
  補上 schema 讀取與 `schemaHasFTS` 判斷，未啟用就回 400，不呼叫 `fts.Search`。

不加這層防呆的話，呼叫端會看到 ftscore 那邊回的「collection not found」，誤以為
collection 本身不存在——但 collection 其實存在，只是沒有 FTS 索引。

### 5. `/collections/list` 補 `has_fts` 欄位

`handleCollectionList`（`handlers.go:362`）裡，schema 解析失敗的分支已經有
`collection["has_fts"] = false`（`handlers.go:439`），但解析成功的分支（`handlers.go:416`
起）從來沒有設過這個欄位——是先前留下的半成品。這次補上：解析成功時用
`schemaHasFTS(schema)` 設定 `has_fts`，讓兩個分支一致。

## 不適用：既有 collection 在 ftscore 端留下的死表

修復前建立的 collection，若 schema 剛好零個 `searchable` 欄位，之前會在 ftscore 那邊建出一張
對應的 FTS5 表；修完之後寫入不再同步到那張表，理論上會變成死資料。**這個情境在部署模型下
不會發生**：每次部署都是重建，不是針對既有 `.indices` 檔案做原地升級，所以不存在「修復前
建立、修復後繼續沿用」這種殘留死表的既有資料。不需要清理工具。

## 測試

整合測試，接真實 embedded 的 ftscore 二進位檔（不引入 mock/interface，維持現有
`query_integration_test.go` 的整合測試風格）：

- 建立一個零 `searchable` 欄位的 collection，寫入文件後直接向 ftscore 查同名 collection，
  驗證回傳「not found」等錯誤，證明從未在 ftscore 建立過。
- 建立一個有 `searchable` 欄位的 collection，驗證行為跟修復前一致（FTS 照常運作），確保這次
  改動沒有動到既有的 FTS 啟用路徑。
- 對零 `searchable` 欄位的 collection 送出帶 search 節點的 `/query` 請求，驗證回 400 而不是
  500 或 ftscore 的原始錯誤字串。
- `/collections/list` 回應驗證 `has_fts` 欄位在兩種 collection 上都正確。
