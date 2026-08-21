# db.sqlite 導入 WAL

## 目標

`db.sqlite`（pocket-fts 自己管的那個資料庫，不含 ftscore 的 `.indices`）導入 WAL 模式，
同時解決兩個問題：

1. **降低 fsync 延遲**——目前用 rollback journal，每次 commit 都 fsync，HDD 上一次約
   100 毫秒（見 [docs/WORKLOG.md](../WORKLOG.md) 缺陷 4）。
2. **讓讀取不被寫入卡住**——rollback journal 下，寫入者 commit 時的 EXCLUSIVE 鎖會瞬間
   擋住所有讀取連線。WAL 模式讀取者不會被寫入者卡住、寫入者也不太會被讀取者卡住。

WAL **不解除** SQLite「同一時間只允許一個寫入者」的限制，所以寫入池
`writeDB.SetMaxOpenConns(1)`（[docs/WORKLOG.md](../WORKLOG.md) 缺陷 4 的排隊修法）繼續
保留，不受這次改動影響。

## 部署面：Docker 從掛載單一檔案改成掛載資料夾

README 四個語言的 Docker 段落目前是 `docker run --rm -v $(pwd)/db.sqlite:/app/db.sqlite`
——只掛載單一檔案。WAL 模式會在同目錄產生 `db.sqlite-wal`／`db.sqlite-shm` 兩個附帶檔案，
單檔掛載下這兩個檔案只會留在容器內部的暫存層，容器一旦 `--rm` 或重啟，還沒 checkpoint
回主檔案的資料就會消失。改成掛載整個資料夾：

```bash
docker run --rm -p 5122:5122 \
  -v $(pwd)/data:/app/data \
  pocket-fts -f /app/data/db.sqlite -p 5122 -host 0.0.0.0
```

`-f` 的用法不變，SQLite 本來就會在同目錄自動長出 `-wal`／`-shm`，不需要新增 flag。

## 決定要做的程式碼變更

### 1. `dsn()` 加 WAL 相關 pragma

```
_pragma=busy_timeout(5000)   // 既有，不變
_pragma=journal_mode(WAL)
_pragma=synchronous(NORMAL)
```

`synchronous=NORMAL` 是 SQLite 官方文件對 WAL 模式的建議值：每次 commit 不 fsync，只在
checkpoint 時 fsync；換來的風險是「作業系統／硬體斷電」時最後幾筆已 commit 的交易可能
遺失（WAL 檔案本身沒事，但還沒 fsync 進磁碟的那幾筆會不見），應用程式自己 crash 不會遺失
資料，只有斷電/系統崩潰才會。

### 2. 啟動時驗證 journal_mode 真的是 WAL

SQLite 在某些情況下（檔案系統不支援、目錄不可寫）會靜默回退到非 WAL 模式，
`PRAGMA journal_mode=WAL` 不報錯，只回傳實際生效的模式名稱。`initDB` 開頭那個連線讀取
`PRAGMA journal_mode` 的回傳值，不是 `wal` 就回錯誤，讓服務啟動失敗，而不是安靜地用
非預期的耐久性模式跑下去。

### 3. Checkpoint 策略

- **平常**：不額外處理，交給 SQLite 內建的自動 checkpoint（WAL 檔案累積到約 1000 頁、
  約 4MB 就自動觸發）。
- **收到 SIGTERM 正常關閉時**：額外執行一次 `PRAGMA wal_checkpoint(TRUNCATE)`，把 WAL
  清空、主檔案寫到最新，讓下次啟動時 WAL 檔案是乾淨的。`main.go` 新增訊號處理，在現有的
  `defer db.Close()`／`defer writeDB.Close()` 之前執行。

### 4. 既有資料相容性

既有的 `db.sqlite`（用舊版本、非 WAL 建立的）不需要任何遷移步驟——`PRAGMA
journal_mode=WAL` 對已存在的 rollback-journal 資料庫是一次性的、自動的轉換，SQLite 原生
支援，不需要額外程式碼處理。

## 測試

- `db.sqlite` 用 `initDB` 開啟後，`PRAGMA journal_mode` 回傳 `wal`。
- 啟動驗證邏輯：用 `:memory:` 資料庫模擬 WAL 靜默回退（`:memory:` 要求 WAL 時實際回傳
  `memory`），驗證這種情況下會回報錯誤，不是靜默放行。
- Checkpoint 函式：寫入資料後呼叫 `PRAGMA wal_checkpoint(TRUNCATE)`，驗證 WAL 檔案被清空
  （檔案大小歸零或接近 0）。

**「讀取不被寫入卡住」不另外寫計時測試。** rollback journal 模式下，寫入者只有在
commit 那一瞬間升級成 EXCLUSIVE 鎖才會擋住新讀取；交易開著但還沒 commit 時（RESERVED
鎖）讀取本來就不受影響。要抓到「commit 那一瞬間」需要讓 commit 本身變慢到可觀測，在單元
測試裡沒有可靠的手段，硬做會是一個間歇性失敗的計時測試。`journal_mode` 確實是 `wal`
這件事本身就直接保證讀取不被寫入卡住——這是 SQLite 文件明載的行為，不需要額外用計時
測試去重新證明它。

## 驗證：重跑壓測確認效能數字（2026-08-21 已完成）

範圍：`pocket-fts-bench`，語料規模 10,000，HDD／NVMe／tmpfs 三種儲存都跑，HDD 為主要
儲存（跑完整讀取階梯），寫入階梯併發 1／8／16／32，每階 15 秒。比較對象是
`pocket_fts_writefix`（[docs/WORKLOG.md](../WORKLOG.md) 缺陷 4 的基準版本，`before`）跟
這次建置的 WAL 版本（`after`）。原始數據：`pocket-fts-bench/results-wal-before.json`／
`results-wal-after.json`。

**HDD 寫入（`W1_upsert`）：**

| 併發 | 修前 rps | 修後 rps | 修前 p50 | 修後 p50 | 修前 p99 | 修後 p99 |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | 10.8 | 19.6（+81%） | 95 ms | 50 ms | 136 ms | 84 ms |
| 8 | 14.1 | 18.5（+31%） | 522 ms | 353 ms | 1241 ms | 1159 ms |
| 16 | 14.8 | 19.0（+28%） | 987 ms | 724 ms | 2972 ms | 2465 ms |
| 32 | 13.7 | 18.3（+34%） | 1975 ms | 1393 ms | 5512 ms | 5417 ms |

**吞吐量與 p50 全面改善**（+28% 到 +81%），符合預期——每次 commit 不再需要 fsync。

**但併發 32 的 p99 幾乎沒變**（5512ms → 5417ms，只降 2%），跟原本設定的「顯著降低 HDD
p99」目標沒有達成。推測原因：寫入池仍然只有一條連線，32 個併發寫入還是得排隊；WAL 檔案
在高併發持續寫入下會成長，SQLite 內建的自動 checkpoint 一旦觸發，會在那條唯一的寫入連線
上做一次同步、需要真的 fsync 的整批寫回，這次 checkpoint 期間排在後面的請求全部被卡住。
p50／吞吐量的改善證明「大部分 commit 不 fsync」這件事確實生效；p99 沒改善，代表「偶爾一次
的 checkpoint 補 fsync」在高併發、深佇列下變成新的尾端延遲來源，效果被排隊本身蓋過去了。
這跟一開始設定的目標（`synchronous=NORMAL` 讓大部分 commit 免 fsync）方向一致，只是排隊
機制在高併發下讓改善反映在平均延遲而不是尾端延遲。

**沒有引入新錯誤**：HDD／NVMe／tmpfs 三種儲存、所有讀寫情境，修前修後都是 0 筆失敗。

**讀取（HDD，主要儲存）**：修前修後幾乎沒有差異（例如 `R4_fts_common` 併發 16 的 p99：
525ms → 583ms，在量測誤差範圍內）。這是預期的——FTS 讀取的瓶頸是 ftscore 自己的單一連線
（[docs/WORKLOG.md](../WORKLOG.md) 第五節「讀取會餓死寫入」的成因），不是 `db.sqlite`
的 journal mode，WAL 對它没有直接影響。

**結論**：這次改動兌現了「降低 fsync 成本」（吞吐量、p50 都顯著改善），但「讀取不被寫入
卡住」的效果在這個 benchmark 情境下看不出來（因為 benchmark 沒有跑會被舊模式鎖住的並發
讀寫組合），而「顯著降低 HDD 高併發 p99」這個附帶目標沒有達成——高併發下的尾端延遲現在
主要卡在寫入池的排隊深度與偶發 checkpoint，不是 fsync 頻率。若要繼續壓低高併發 p99，
下一步應該是調整 `wal_autocheckpoint` 的頁數門檻（讓 checkpoint 更頻繁但每次更小），或
評估寫入池是否該從單一連線放寬——但後者會重新引入 SQLite 鎖爭用的風險，需要另外討論。
