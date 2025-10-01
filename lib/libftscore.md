# ftscore 使用與 API 指南

本文件整合原本的 `API.md` 與 `USAGE.md`，詳細說明如何在 Go 專案中使用 `ftscore/engine`，並列出常用的公開 API。

> 若需要確認 Version API 版本，可呼叫 `engine.Version()`（初始為 `v0.3`，每次執行 `release.sh` 會自動將版本的「小號」加一）。

### 1.1 Version API

`engine.Version()` 會回傳目前 Version API 的語義版本字串，例如：

```go
log.Printf("version api=%s", engine.Version())
```

這個版本號會在執行 `./release.sh` 時自動遞增（小號 +1），方便外部系統判斷功能集或進行相容性檢查。

## 1. 初始化引擎

```go
ctx := context.Background()

eng, err := engine.New(ctx, "data/service.db", logging.StdLogger(), 5*time.Second, true)
if err != nil {
    log.Fatal(err)
}
defer eng.Close()
```

引擎初始化參數說明：

- `dbPath`：必填。支援相對路徑，最終會轉成絕對路徑並建立目錄。實際檔名會沿用原始名稱但將副檔名改為 `.indices`（例如 `service.db` -> `service.indices`）。
- `logger`：實作 `logging.Logger` 介面；傳入 `nil` 時改用 `logging.Noop()`。所有 SQL 都會以 Debug 等級輸出。
- `busyTimeout`：SQLite busy timeout。0 代表使用預設 5 秒。
- `stemming`：是否在建立 collection 時啟用英文 Porter 詞幹化。啟用後寫入與搜尋的 token 都會轉為小寫再進行 bigram/porter 處理。
- `engine.Version()`：取得 Version API 版本字串，可用於外部程式判斷能力差異。

啟動時 engine 會套用下列 PRAGMA，以降低 I/O 並確保只由單一進程存取：

- `synchronous=NORMAL`
- `temp_store=MEMORY`
- `cache_size=-20000`
- `journal_size_limit=67108864`
- `mmap_size=134217728`
- `foreign_keys=ON`
- `locking_mode=EXCLUSIVE`
- `journal_mode=MEMORY`（交易日誌僅存在記憶體，磁碟不會產生 `*-journal` 檔案）

> 注意：引擎擁有 SQLite 連線的生命週期，請勿自行在外部重複使用或關閉底層連線。

## 2. Collection 架構

### 2.1 建立 Schema

```go
schema := engine.CollectionSchema{
    Name:       "articles",
    PrimaryKey: "id",
    Fields: []engine.Field{
        {Name: "id", Type: engine.FieldTypeText},
        {Name: "title", Type: engine.FieldTypeText, Weight: 2},
        {Name: "body", Type: engine.FieldTypeText},
        {Name: "published_at", Type: engine.FieldTypeInteger, Indexed: true},
    },
    FTS: engine.FTSOptions{Stemming: true},
}

if err := eng.CreateCollection(ctx, schema); err != nil {
    log.Fatal(err)
}
```

Schema 驗證與規則：

- collection 與欄位名稱須符合 `[A-Za-z_][A-Za-z0-9_]*`。
- 必須指定主鍵欄位，且不可 nullable。
- 至少需要一個文字欄位才能建立 FTS 表。
- 每個文字欄位可選填 `Weight`（預設 1），會映射到 FTS `bm25` 權重。非文字欄位的 `Weight` 會自動視為 0。
- `FTS.Stemming=true` 時，FTS 表使用 `tokenize=porter`，並搭配內建 bigram token 及小寫化流程。
- `CreateCollection` 會同步建立主表、FTS 虛擬表、觸發器與必要索引。`DeleteCollection` 則會移除所有關聯物件。

可用 API：

- `eng.CreateCollection(ctx, schema)`：建立新 collection。
- `eng.DeleteCollection(ctx, name)`：刪除 collection 與 metadata。
- `eng.ListCollections(ctx)` / `eng.CollectionCatalog(ctx)`：檢視目前的 collection 資訊。
- `eng.Reload(ctx)`：重新掃描 metadata 並刷新快取。

## 3. 取得 Collection Handle

```go
articles, err := eng.Collection(ctx, "articles")
if err != nil {
    log.Fatal(err)
}
```

`CollectionHandle` 提供對單一 collection 的操作：

| 方法 | 說明 |
| ---- | ---- |
| `Upsert(ctx, doc engine.Document)` | 依主鍵插入或更新單筆文件。 |
| `UpsertMany(ctx, docs []engine.Document)` | 逐筆呼叫 `Upsert`，遇到第一個錯誤即停止。 |
| `Delete(ctx, id any)` | 依主鍵刪除資料列。若不存在會回傳 `CodeNotFound`。 |
| `Get(ctx, id any)` | 讀取并回傳文件內容（`map[string]any`）。 |
| `Search(ctx, req engine.SearchRequest)` | 執行全文搜尋並回傳 `engine.SearchResult`。 |

寫入時的注意事項：

- 文字欄位會先經過 bigram token 化，再依 `stemming` 設定轉為小寫並套用 Porter。
- 非空欄位在首次插入時必須提供；之後的 Upsert 可只附帶需要更新的欄位。

## 4. 搜尋 API

```go
result, err := articles.Search(ctx, engine.SearchRequest{
    Query:   "中文 檢索",
    Limit:   10,
    Offset:  0,
    Weights: map[string]float64{"title": 3}, // 可選：調整本次查詢的欄位權重
})
if err != nil {
    log.Fatal(err)
}
for _, hit := range result.Hits {
    fmt.Printf("ID=%s score=%.4f\n", hit.ID, hit.Score)
}
```

`SearchRequest` 欄位說明：

| 欄位 | 型別 | 說明 |
| ---- | ---- | ---- |
| `Query` | `string` | 必填。會拆成 tokens 後套用 FTS `MATCH`。空白代表 AND，支援 FTS5 運算子 (`OR`、`NOT`、`NEAR`)。 |
| `Limit` | `int` | 預設 20。需 > 0。 |
| `Offset` | `int` | 預設 0。若 < 0 會自動調整為 0。 |
| `Filters` | `map[string]any` | 尚未實作，傳入會得到 `CodeInvalid`。 |
| `OrderBy` | `[]engine.OrderExpression` | 尚未實作，傳入會得到 `CodeInvalid`。 |
| `Weights` | `map[string]float64` | 選用。以 `{欄位: 權重}` 覆蓋本次查詢的 `bm25` 權重，需為正值；未指定的欄位沿用 schema 設定。 |
| `ReturnFields` | `[]string` | 選用。列出本次查詢要返回的欄位名稱；如未指定則僅回傳主鍵欄位與 `_score`。 |

搜尋行為：

- 查詢文字會先套用 bigram；若 collection 啟用 stemming，也會轉成小寫。
- 結果固定依 `bm25` 分數排序，數值越小代表越相關。
- 回傳結果預設提供主鍵欄位與 `_score`；若需要其他欄位請在 `ReturnFields` 指定。
- `SearchResult` 結構包含：
  - `Hits`：每筆命中包含 `ID`、`Score`、`Fields`（原主表欄位資料）。
  - `Total`：符合條件的總筆數。
  - `ExecutionTime`：執行時間。
  - `Partial`：若只回傳部分欄位則為 `true`。

## 5. 錯誤處理

`ftscore/internal/errors` 將常見錯誤分類為幾種 `Code`：

| Code | 使用情境 |
| ---- | ---- |
| `invalid` | 參數錯誤、欄位缺失或尚未支援的功能（例如 Filters/OrderBy）。 |
| `not_found` | 查無資料（如 `Get`、`Delete` 或 `Collection`）。 |
| `conflict` | 主鍵重複、約束衝突。 |
| `internal` | 其他未分類錯誤，通常來自底層 SQLite。 |

常用輔助函式：

- `errors.CodeOf(err)`：取出錯誤碼。
- `errors.IsCode(err, errors.CodeNotFound)`：判斷是否為特定錯誤碼。

所有 SQLite 錯誤都會被包裝成上述錯誤碼，方便呼叫端判斷。

## 6. 實用建議

- 建議以 Go 的 context 控制逾時或取消，避免長時間持有鎖。
- 若需要批次寫入，可先用 `UpsertMany`；內部會順序執行 `Upsert`，失敗會回傳第一個錯誤。
- 測試環境可搭配 SQLite `:memory:` 或 `file:xxx?mode=memory&cache=shared` 使用，再透過 `storage.Open`。
- 若要額外記錄 SQL，可提供自訂 logger，或將 logger 設為 `logging.StdLogger()` 以輸出到標準輸出。

## 7. 與其他 Go 專案整合

`release.sh` 會輸出兩種成果：

1. **Go 模組原始碼**（`dist/ftscore/` 與 `dist/ftscore-module.tar.gz`）可供其他 Go 專案透過 `replace` 匯入。
2. **C Bindings**（`dist/c/libftscore.so` 與 `dist/c/libftscore.h`）可讓 C/C++ 或其他支援 C FFI 的語言直接呼叫。

執行流程：

1. 在專案根目錄執行：

   ```bash
   ./release.sh
   ```

   完成後會看到：

   ```
   dist/
     ├─ ftscore/        # 可供 replace 使用的 Go 模組來源
     └─ ftscore-module.tar.gz
   ```

2. 在要整合 ftscore 的其他專案內，於 `go.mod` 加入：

   ```go
   module your/service

   go 1.25

   require ftscore v0.0.0

   replace ftscore => /absolute/path/to/ftscore/dist/ftscore
   ```

   > 路徑請改成實際的 `dist/ftscore` 絕對路徑。

3. 在程式碼中即可直接匯入使用：

   ```go
   import "ftscore/engine"
   ```

4. 若要分享給沒有共享檔案系統的同事，可將 `dist/ftscore-module.tar.gz` 解壓到任意資料夾，再使用相同的 `replace` 指向解壓後的位置。

5. 建議定期重新執行 `./release.sh` 以確保 `dist/ftscore` 與原始碼同步。

### 7.1 C 語言介面說明

`dist/c/` 會包含 `libftscore.so` 與對應的 `libftscore.h`（Windows 編譯時會輸出 `.dll`）。匯出的函式以 JSON 當作交換格式，減少跨語言資料結構轉換的負擔：

| 函式 | 說明 |
| ---- | ---- |
| `unsigned long long FtsEngineNew(const char* db_path, long long busy_timeout_ms, int stemming, char** err)` | 建立引擎並回傳 handle，`err` 不為 `NULL` 時會填入錯誤訊息（需呼叫 `FtsFree` 釋放）。 |
| `int FtsEngineClose(unsigned long long handle, char** err)` | 關閉並移除指定引擎。 |
| `int FtsCreateCollection(unsigned long long handle, const char* schema_json, char** err)` | 透過 JSON Schema 建立 collection（同 `CollectionSchema` 格式）。 |
| `int FtsUpsertDocument(unsigned long long handle, const char* collection_name, const char* document_json, char** err)` | 將 JSON 文件寫入/更新指定 collection。 |
| `int FtsDeleteDocument(unsigned long long handle, const char* collection_name, const char* primary_key_json, char** err)` | 根據主鍵 JSON（例如 `{ "id": "1" }`）刪除資料列。 |
| `int FtsDeleteCollection(unsigned long long handle, const char* collection_name, char** err)` | 刪除指定 collection 與相關 metadata。 |
| `int FtsSearch(unsigned long long handle, const char* collection_name, const char* request_json, char** result_json, char** err)` | 執行搜尋並以 JSON 回傳 `SearchResult`；呼叫端必須在使用完後呼叫 `FtsFree(*result_json)`。 |
| `void FtsSetLogCallback(fts_log_cb_t cb, void* user_data)` | 設定全域日誌回呼；回呼簽名為 `void (*cb)(int level, const char* message, void* user_data)`，其中 level 依序代表 Debug(0)/Info(1)/Warn(2)/Error(3)。 |
| `char* FtsVersion()` | 取得 Version API 版本字串（需呼叫 `FtsFree` 釋放）。 |
| `void FtsFree(void* ptr)` | 釋放由前述函式分配的字串記憶體。 |

C 範例（省略錯誤處理）：

```c
#include "libftscore.h"
#include <stdio.h>

static void log_cb(int level, const char* message, void* user_data) {
    (void)user_data;
    printf("[level=%d] %s\n", level, message ? message : "");
}

int main() {
    char *err = NULL;

    FtsSetLogCallback(log_cb, NULL);

    char *version = FtsVersion();
    if (version) {
        printf("Version=%s\n", version);
        FtsFree(version);
    }

    unsigned long long eng = FtsEngineNew("data/service.db", 5000, 1, &err);
    if (err) { fprintf(stderr, "%s\n", err); FtsFree(err); return 1; }

    const char *schema = "{\"name\":\"articles\",\"primary_key\":\"id\","
                         "\"fields\":[{\"name\":\"id\",\"type\":\"text\"}]}";
    if (FtsCreateCollection(eng, schema, &err) != 0) { fprintf(stderr, "%s\n", err); FtsFree(err); }

    const char *doc = "{\"id\":\"1\",\"title\":\"hello\"}";
    FtsUpsertDocument(eng, "articles", doc, &err);

    const char *query = "{\"query\":\"hello\",\"return_fields\":[\"id\",\"title\"]}";
    char *result = NULL;
    FtsSearch(eng, "articles", query, &result, &err);
    if (result) { printf("%s\n", result); FtsFree(result); }

    FtsEngineClose(eng, &err);
}
```

### 7.2 JSON 範例

以下列出常用 API 所需的 JSON 格式，可直接套用或擴充：

- **Collection Schema（`FtsCreateCollection`）**

  ```json
  {
    "name": "articles",
    "primary_key": "id",
    "fts": {"stemming": true},
    "fields": [
      {"name": "id", "type": "text", "weight": 2},
      {"name": "title", "type": "text"},
      {"name": "body", "type": "text", "weight": 0.5},
      {"name": "published_at", "type": "integer", "indexed": true}
    ]
  }
  ```

- **Document Upsert（`FtsUpsertDocument`）**

  ```json
  {
    "id": "a-1001",
    "title": "Go 中文全文檢索",
    "body": "使用 bigram 與 porter 建立跨語系搜尋",
    "published_at": 20250101,
    "tags": ["golang", "fts5"]
  }
  ```

  > 欄位名稱需與 schema 定義一致；未宣告的欄位會被忽略。

- **Document Delete（`FtsDeleteDocument`）**

  ```json
  {
    "id": "a-1001"
  }
  ```

- **Search Request（`FtsSearch`）**

  ```json
  {
    "query": "中文 檢索",
    "limit": 10,
    "offset": 0,
    "weights": {
      "title": 3,
      "body": 1
    },
    "return_fields": ["id", "title", "body"]
  }
  ```

-  > `filters` 與 `order_by` 目前尚未實作，傳入會得到 `invalid` 錯誤。

透過這組 API，其他語言只需準備 JSON 字串即可進行 collection 建立、寫入與搜尋（`FtsDeleteCollection` 例外，僅需傳入 collection 名稱字串）。記得在處理完回傳字串後呼叫 `FtsFree` 釋放記憶體。

---

如需更多執行範例，可參考 `src/cmd/` 目錄下的示範程式與整合測試。
