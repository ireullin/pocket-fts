# 03 — 抽出 `Store` 與 Go API，讓 pocket-fts 可以當函式庫匯入（隱藏功能）

**What to build:** 把現在寫在 HTTP handler 裡的邏輯（schema 驗證、SQL 產生、寫入、
查詢）搬進一個可以 `import` 的 package，讓特殊需求的專案可以在自己的行程裡內嵌
pocket-fts，不必另外跑服務。HTTP 服務仍然是主力，改成呼叫這個 package 的薄層，
對外行為完全不變。

這是**隱藏功能**：README 與 API_REFERENCE 不提函式庫，只記錄在這張票與程式碼註解。

來源：2026-09-25 grilling。原始建議文件主張廢除 HTTP、直接 import ftscore；使用者裁定
HTTP 保留為主力、ftscore 不改、兩邊都用 dlopen。

**Blocked by:** None — can start immediately。
[04](./write-lock.md)、[05](./documents-update.md)、[06](./field-primary-key-flag.md)、
[07](./score-limit-pushdown.md) 排在這張之後，直接實作在 `Store` 裡。

**Status:** closed（2026-09-25）

## 已定案的設計

- 同一個倉庫、master，不開長期分支，不開新專案。
- module path `pocket_fts` → `github.com/ireullin/pocket-fts`。核心在 `pocketfts/`，
  HTTP 服務在 `cmd/pocket_fts`。
- `Open(Config) (*Store, error)`／`(*Store) Close() error`。`Config`：資料庫路徑、寫入
  逾時、`*slog.Logger`、FTS WAL（預設關閉，對應隱藏旗標 `-fts-wal`）。錯誤一律回傳，
  不呼叫 `os.Exit`，不處理訊號。
- 全域變數（`db`、`writeDB`、`logger`、`fts`、`queryExecutor`、`writeTimeout`）收進
  `Store`。
- API 全部是單筆：`CreateCollection`、`DeleteCollection`、`ListCollections`、`Upsert`、
  `Delete`、`Query`（`Node` 對應巢狀格式的 `query` 樹）、`ParseNode([]byte)`。
  `Update` 由 [05](./documents-update.md) 加入。
- 函式庫與 HTTP 都用 dlopen 載入內嵌的 ftscore。ftscore 不改。
- 程式碼註解寫明：`FtsSetCallTimeout` 與 log callback 是整個行程共用；一個行程只開一個
  `Store`；預設模式下一個 `.indices` 同時只能由一個行程開啟。
- HTTP 服務的訊號處理與 `pocket_fts.log` 維持現狀（既有程式碼不回頭改）。

## 不做

- 批次寫入 API、`Reindex`、權限／多租戶、查詢樹的安全驗證、刪除 HTTP 層。

## 驗收條件

- [x] 其他 module 可以 `import "github.com/ireullin/pocket-fts/pocketfts"`，用上面的 API
      完成建 collection、寫入、全文搜尋、刪除、關閉（寫一個外部 module 的範例測試證明）。
- [x] `cmd/pocket_fts` 的 handler 只做 HTTP 解析與回應，邏輯全部呼叫 `Store`。
- [x] 既有測試全部改成對新結構執行，且全部通過，證明 HTTP 行為不變。
- [x] 用重建後的 `bin/pocket_fts` 起真實服務，跑一次建立／寫入／查詢／排序／刪除（e2e）。
- [x] `Dockerfile`、README 的建置指令改成新的建置路徑；`podman build` 與 `podman run` 實測。
- [x] README 與 API_REFERENCE 沒有提到函式庫。

## 關票核對（2026-09-25）

實作於 `2a3a2de`，審查修正 `eb462c2`，執行檔 `b4dd87e`。逐條核對：

1. 外部 module 可匯入：在 repo 外建一個 `module example.com/host`（`replace` 指向本 repo），測試 `TestEmbed` 走完 Open → CreateCollection → Upsert → Query（搜到 1 筆）→ Delete → ListCollections → Close，通過。repo 內另有 `pocketfts/example_test.go`（`package pocketfts_test`）的 `Example`，輸出核對通過。層級：跨 module 實際建置與執行。
2. `cmd/pocket_fts/handlers.go` 只剩 JSON 解析與狀態碼對應，全部呼叫 `store.*`。層級：程式碼檢視。
3. 既有 80 個測試搬到 `pocketfts/` 與 `cmd/pocket_fts/` 後全部通過；只有 `TestSetWriteTimeoutIgnoresNonPositive` 改寫成 `TestOpenIgnoresNonPositiveWriteTimeout`（全域變數改成 Config）。層級：單元／整合測試。
4. 真實服務 e2e：新舊執行檔對同一組 49 個請求（建立、錯誤請求、upsert、list、content、query、search、delete）的回應，除了 `/search` 的 `ExecutionTime` 之外逐位元組相同（票 04 之後另有一處訊息差異，見 04）。另外 10 萬筆語料上 7 組查詢的回應逐位元組相同。層級：e2e。
5. `README.md` 四個語言的建置指令改成 `./cmd/pocket_fts`；Dockerfile 只複製 `bin/pocket_fts`，不需要改。`podman build` 後 `podman run`，容器內建立、upsert、update（200 與 409）、依 `_score` 查詢、刪除都正常，容器 log 的 ERROR 為 0。層級：容器實測。
6. `grep -i 'pocketfts\|library'` README.md、API_REFERENCE.md 沒有結果。層級：文件檢視。
