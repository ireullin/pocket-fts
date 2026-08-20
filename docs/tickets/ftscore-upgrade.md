# ftscore 升級：pocket-fts 程式碼因應

`src/embedded/ftscore` 已替換成新版二進位檔（未 commit 的 unstaged 變更），內含 8 個
commit，修正一次正式環境卡死事故的根因、一個 primary key 資料查找正確性 bug，並做了
幾項效能優化。本 ticket 只處理 **pocket-fts 這邊需要因應的程式碼變更**，不含既有
`.indices` 資料的遷移（若之後需要遷移既有 PK 被誤轉換的資料，另開遷移工作、並參照
ftscore 專案本身提供的遷移指南）。

## 背景：新版 ftscore 的行為變更

1. `FtsUpsertDocument`／`FtsDeleteDocument`／`FtsSearch` 新增預設 10 秒呼叫逾時，可用新
   函式 `FtsSetCallTimeout(ms)` 調整（單一 process-wide 設定，不含 handle 參數；已用
   `objdump` 反組譯確認呼叫慣例只傳一個 8-byte 參數）。
2. Primary key 不再被 bigram 轉換／不再轉小寫、不再收錄進 FTS5 索引；schema 若「唯一文
   字欄位就是 PK」，`CreateCollection` 會直接回錯誤。
3. `UpsertMany` 改成單一 transaction（具原子性）。

## pocket-fts 現況盤點

- `src/fts_dl.go` 的 dlsym 清單完全沒有 `FtsSetCallTimeout`，升級後會直接吃到新 ftscore
  的預設 10 秒逾時，且目前沒有調整的手段。
- `main.go:99` 呼叫 `NewFTS(*dbFile, 5000, true)`，stemming 對整個 engine 寫死為
  `true`，套用到所有 collection。
- `handleCollectionCreate`（`handlers.go:257`）直接把 request body 傳給
  `fts.CreateCollection`，失敗時已把 ftscore 回傳的錯誤字串包成 500 回給呼叫端。
- 專案內沒有任何 `UpsertMany` 或批次匯入端點（只呼叫單筆 `UpsertDocument`）。
- `initDB()`（`src/database.go`）**完全沒有設定 `busy_timeout`**（連 DSN 都沒帶
  pragma），比另一個系統的 ftscore 升級中發現的「busy_timeout 只套用到單一連線」還
  嚴重。Go 的 `database/sql` 連線池（讀 schema／原始文件）與 ftscore C engine 自己的連
  線（`busyTimeoutMs=5000`，只套用在 ftscore 那一側）同時打同一個 `db.sqlite`，Go 側任
  何鎖爭用會立即回傳 `database is locked`，不會等待。這不是這批 ftscore commit 帶來的
  變更，是這次升級的高併發驗證情境類比出的既有 Go 側問題，決定一併修掉。

## 決定要做的程式碼變更

### 1. 新增 `FtsSetCallTimeout` 綁定（不呼叫、不對外暴露參數）

- `fts_dl.go`：新增 typedef、`dlsym` 查找（維持跟其他符號一致的「找不到就視為載入失敗」
  慣例）、C wrapper `call_fts_set_call_timeout`。
- `fts_wrapper.go`：新增對應的 Go 函式（package-level，非 `*FTS` 方法，因為底層符號沒有
  handle 參數）。
- **不**在 `main.go` 呼叫它、**不**新增 CLI 參數。維持 ftscore 內建的 10 秒預設值。之後
  若實測發現合法操作（大批次寫入／大資料集搜尋）會被 10 秒卡到，再回來決定要不要暴露成
  啟動參數。

> **2026-08-20 後續：這個待決事項已結案。** 壓力測試實測到了當初預期的情境：48 個併發
> 寫入者時，576 筆有 10 筆被 ftscore 的 10 秒判死，回傳
> `insert into docs: context deadline exceeded`。同一批測試也顯示 100 萬筆語料上搜尋
> 常見詞的 p99 是 8.5 秒，距離 10 秒的天花板很近，搜尋很可能已經偶發失敗而沒被發現。
>
> 決定：新增 `-write-timeout` 啟動參數（秒，預設 30），`main.go` 在初始化 FTS 引擎之前
> 呼叫 `SetCallTimeout(writeTimeout)`，讓寫入路徑的兩個階段（FTS 索引與 SQL 表）共用
> 同一個預算。`SetCallTimeout` 是 process-wide，所以搜尋也套用同一個值。
>
> 驗證：同樣的 48 併發情境，預設 30 秒下 576 筆全部成功；設成 1 秒則 358 筆回 `503`，
> 證明參數確實貫穿到 ftscore 那一側。

### 2. 修 `initDB()` 的 busy_timeout

- 改用 DSN 帶 `_pragma=busy_timeout(5000)`，讓 `modernc.org/sqlite` 對連線池裡每一條新
  連線都自動套用，不再只套用到剛好執行那次 `Exec` 的連線。
- 5000ms 沿用現有傳給 ftscore engine 的 `busyTimeoutMs` 數值，兩側維持一致。

### 3. Schema 驗證：不加 pocket-fts 側的事前檢查

- 維持現況，讓 `CreateCollection` 失敗時 ftscore 的錯誤訊息直接透過既有的 500 錯誤處理
  回傳，不在 pocket-fts 這層重複實作「PK 是否為唯一文字欄位」的驗證邏輯。

### 4. `UpsertMany` 原子性變更：不適用

- pocket-fts 沒有呼叫 `UpsertMany`，此行為變更不影響現有程式碼，不需要改動。

## 測試

第一批 `_test.go`（專案目前沒有任何測試）：

- `src/database_test.go`：驗證 `initDB()` 回傳的 `*sql.DB` 在併發寫入下會依 busy_timeout
  等待重試，而不是立即回傳 `database is locked`。
- `src/fts_dl_test.go`：驗證新的 `FtsSetCallTimeout` dlsym 綁定能成功載入並呼叫（透過
  `LoadFTSLibrary` 載入 embedded 的新版 ftscore 二進位檔），不需要真的驗證逾時行為本
  身（那是 ftscore 自己的職責）。

## 不在這次範圍內

- 既有 `.indices` 資料的 PK 遷移（受影響條件：CJK 3 字以上，或 stemming 開啟且 PK 大小
  寫混合——pocket-fts 目前 stemming 對所有 collection 都是 `true`，需要另外盤點實際資料
  才能判斷是否受影響）。
- 是否要把 `FtsSetCallTimeout` 暴露成可調參數。
- pocket-fts 側 schema 建立前的事前驗證。

以上三項待這次程式碼因應完成、且有實際資料/使用情境後再另外決策。
