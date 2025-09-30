# 目的
1. 這是一個全文搜索引擎
2. 透過http的方式進行操作
3. 索引的儲存與建立透過外部c-libray進行,相關說明都在lib目錄
4. 本程式會自己維護一個sqlite檔案,用作一般關聯式資料庫使用
5. 所有程式碼存放在src目錄中
6. 交談都使用中文
7. 有疑問的地方都停下來問
8. 每一次動作前都重新閱讀 PLAN.md

# 啟動參數
- -p: 啟動的port 預設5122
- -f: db存放的位置,預設為程式目錄下 db.sqlite

# 技術使用
- go
- sqlite3
- modernc.org
- vanilla js

# 進度
## 1. api設計
Collection Create
```
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

Collection Delete
```
{
  "name": "articles"
}
```

Document Upsert
```
{
  "id": "a-1001",
  "title": "Go 中文全文檢索",
  "body": "使用 bigram 與 porter 建立跨語系搜尋",
  "published_at": 20250101,
  "tags": ["golang", "fts5"]
}
```

Document Delete
```
{
  "id": "a-1001"
}
```

Search
```
{
  "query": "中文 檢索",
  "limit": 10,
  "offset": 0,
  "weights": {
    "title": 3,
    "body": 1
  }
}
```

## 2. api 內部邏輯
1. 系統自行維護一張表名為collections 紀錄Collection Create 時的資訊
2. 根據以上API內容操作sqlite
3. 直接將接收到的json字串傳入c-library對應的api中
4. 將api返回的數據與sqlite操作的結果一併返回


## 3. http操作頁面
1. 建立視覺化的api 操作介面

# 開發規劃
## 第一階段：專案初始化與基礎建設 (完成於 2025-09-30)
- **建立 `src` 目錄**
    - 執行指令 `mkdir src`
- **初始化 Go 模組**
    - 執行指令 `mise exec -- go mod init pocket_fts`
- **建立 `src/main.go` 檔案並實作基礎伺服器**
    - 寫入 Go 程式碼，建立一個基本的 HTTP 伺服器。
    - 使用 `flag` 套件來處理 `-p` (port) 和 `-f` (db file) 兩個啟動參數。
    - 伺服器根路徑 `/` 會回傳 "Pocket FTS is running."。

## 第二階段：整合 C 函式庫
1. 研究 `lib/libftscore.h` 檔案，了解需要呼叫的 C 函式簽名。
2. 建立一個 Go 檔案 (例如 `src/fts_wrapper.go`)，使用 `cgo` 來載入 `libftscore.so` 並封裝 C 函式的呼叫，使其在 Go 中可以方便使用。

## 第三階段：資料庫層建置
1. 引入 `modernc.org/sqlite` 套件。
2. 建立一個 Go 檔案 (例如 `src/database.go`) 來處理所有與 SQLite 的互動。
3. 實作初始化資料庫、建立 `collections` 表格，以及對 `collections` 表格的增刪查改功能。

## 第四階段：API 邏輯實作
1. 根據 `PLAN.md` 中的 API 設計，為五個主要操作（Collection 增/刪，Document 增/刪，Search）分別建立對應的 HTTP Handler。
2. 在 Handler 中，解析傳入的 JSON，並呼叫第二、三階段建立的模組來完成對 C 函式庫和 SQLite 的操作。
3. 組合 C 函式庫和資料庫的回傳結果，以 JSON 格式回應給客戶端。

## 第五階段：前端頁面開發
1. 建立一個 `static` 或 `public` 目錄來存放靜態檔案。
2. 建立 `index.html` 和 `app.js`。
3. 在 HTML 中設計表單，對應 API 的各種操作。
4. 使用 JavaScript 撰寫 `fetch` 請求，非同步地呼叫後端 API，並將結果顯示在頁面上。
5. 在 Go 伺服器中加入提供靜態檔案服務的功能。