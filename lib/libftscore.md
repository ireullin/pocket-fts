
C 語言介面說明
包含 `libftscore.so` 與對應的 `libftscore.h`（Windows 編譯時會輸出 `.dll`）。匯出的函式以 JSON 當作交換格式，減少跨語言資料結構轉換的負擔：

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
| `void FtsFree(void* ptr)` | 釋放由前述函式分配的字串記憶體。 |
