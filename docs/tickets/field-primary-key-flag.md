# 06 — 欄位上的 `primary_key` 旗標讓 FTS collection 建立失敗（HTTP 500）

## 缺陷（2026-09-25 e2e 發現）

API_REFERENCE 的 Field object 把 `"primary_key": true` 寫成可用的便利旗標。可是
`schemaForFTS`（`src/handlers.go:990`）把它原樣轉給 ftscore，ftscore v0.15 拒絕：

```
Failed to create FTS collection ... error: field definition must not contain primary_key; specify it at the schema level
```

pocket-fts 回 500。只有帶 searchable 欄位的 collection 會失敗；沒有 searchable 欄位的
collection 不經過 ftscore，建立成功。同一個 schema 因為有沒有全文索引而結果不同。

## 已定案的設計

- 保留這個旗標。轉給 ftscore 時不送 `primary_key`。
- 旗標標在非主鍵欄位（跟頂層 `primary_key` 不一致）時回 400，不建立任何東西。

**Blocked by:** [03](./store-library.md)

**Status:** closed（2026-09-25）

## 驗收條件

- [x] 先寫一個測試重現 500，確認修改前失敗。
- [x] 帶 searchable 欄位、主鍵欄位標 `primary_key: true`：建立成功，搜尋正常。
- [x] 旗標標在非主鍵欄位：400，資料表、schema、FTS collection 都沒有建立。
- [x] `schemaForFTS` 的輸出不含 `primary_key` 欄位旗標（單元測試）。
- [x] 真實服務 e2e：同一個 schema 在有／無 searchable 欄位時都建立成功。

## 關票核對（2026-09-25）

實作於 `799394e`，審查修正 `eb462c2`。逐條核對：

1. 修改前重現：`TestFieldPrimaryKeyFlagOnFTSCollection` 修改前失敗，回 500 `Failed to create FTS collection: field definition must not contain primary_key; specify it at the schema level`。層級：整合測試。
2. 修改後建立 201、upsert、搜尋找到 f1。層級：整合測試（真實 ftscore）。
3. 標在非主鍵欄位 400、什麼都不建：`TestFieldPrimaryKeyFlagMismatchIsRejected` 驗證沒有 schema、沒有資料表；FTS collection 的部分，審查發現原本用 `Search` 驗證是空驗，已改成「同名重新建立成功」來證明（ftscore 拒絕重複建立，由 `TestCreatingAnExistingFTSCollectionFails` 釘住）。層級：整合測試。
4. `TestSchemaForFTSDropsFieldPrimaryKeyFlag` 驗證 payload 的欄位不含 `primary_key`。層級：單元測試。
5. 真實服務 e2e：同一個 schema 有 searchable 欄位 201、沒有 201，標錯欄位 400；有 FTS 的那個 upsert 後搜得到。層級：e2e。
