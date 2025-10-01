# Pocket FTS

一個高效能的全文搜尋引擎，結合 Go HTTP API 與 C 函式庫，支援複雜查詢和 FTS 分數整合。

## 快速開始

### 1. 啟動服務器
```bash
cd src
LD_LIBRARY_PATH=../lib go run .
```

### 2. 載入測試資料
```bash
go build -o data_loader data_loader.go
./data_loader
```

### 3. 測試查詢
```bash
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "search": {
        "term": "無線耳機"
      }
    },
    "result": {"limit": 5}
  }'
```

## 文檔

- 📖 [CLAUDE.md](./CLAUDE.md) - 完整開發指南和 API 文檔
- 📊 [DATA_LOADER.md](./DATA_LOADER.md) - 資料載入工具使用說明
- 📋 [PLAN.md](./PLAN.md) - 專案計劃和架構設計

## 主要功能

- ✅ 全文搜尋 (FTS) 與 BM25 評分
- ✅ SQL 查詢與複雜條件過濾
- ✅ 混合查詢 DSL (SQL + FTS)
- ✅ FTS 分數與 SQL 結果整合
- ✅ 即時日誌記錄與版本追踪
- ✅ TSV 批量資料載入

## 技術棧

- **後端**: Go + CGO
- **搜尋引擎**: 自訂 C 函式庫 (libftscore)
- **資料庫**: SQLite (modernc.org/sqlite)
- **API**: HTTP/JSON REST API
- **日誌**: 結構化 JSON 日誌