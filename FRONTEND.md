# Pocket FTS 前端管理介面

## 功能介紹

這是 Pocket FTS 的網頁管理介面，提供完整的視覺化操作功能，讓您可以輕鬆管理 Collections、Documents 和進行搜尋操作。

## 啟動方式

### 1. 啟動服務器
```bash
cd src
LD_LIBRARY_PATH=../lib go run .
```

### 2. 訪問管理介面
打開瀏覽器，訪問：
```
http://localhost:5122
```

系統會自動重導向到管理介面。

## 主要功能

### 📁 Collection 管理

#### 建立 Collection
1. 輸入 Collection 名稱（如：`articles`）
2. 輸入主鍵欄位名稱（如：`id`）
3. 手動輸入 JSON Schema 或點擊「自動生成 Schema」
4. 點擊「建立 Collection」

**Schema 範例：**
```json
{
  "name": "articles",
  "primary_key": "id",
  "fts": {"stemming": true},
  "fields": [
    {"name": "id", "type": "text"},
    {"name": "title", "type": "text", "weight": 2},
    {"name": "body", "type": "text", "weight": 1},
    {"name": "published_at", "type": "integer"}
  ]
}
```

#### 刪除 Collection
1. 輸入要刪除的 Collection 名稱
2. 點擊「刪除 Collection」
3. 確認刪除操作

### 📄 Document 管理

#### 新增/更新 Document
1. 輸入 Collection 名稱
2. 輸入 Document JSON 資料
3. 點擊「新增/更新 Document」

**Document 範例：**
```json
{
  "id": "a-1001",
  "title": "Go 中文全文檢索",
  "body": "使用 bigram 與 porter 建立跨語系搜尋",
  "published_at": 20250101
}
```

#### 刪除 Document
1. 輸入 Collection 名稱
2. 輸入要刪除的 Document ID
3. 點擊「刪除 Document」

### 🔍 搜尋功能

#### FTS 搜尋
1. 輸入 Collection 名稱
2. 輸入搜尋關鍵字
3. 設定結果數量限制
4. 點擊「執行搜尋」

#### 進階查詢 (DSL)
支援複雜的查詢語法，包含 SQL 條件和 FTS 搜尋的組合。

**進階查詢範例：**
```json
{
  "collection": "articles",
  "query": {
    "$and": [
      {"sql": {"where": {"published_at": {"$gt": 20240101}}}},
      {"search": {"term": "搜尋"}}
    ]
  },
  "result": {"limit": 5}
}
```

## 介面特色

### 🎨 設計特點
- **響應式設計**：支援桌面和行動裝置
- **即時回饋**：操作結果即時顯示
- **錯誤處理**：清楚的錯誤訊息提示
- **範例支援**：內建範例資料和查詢

### ⚡ 操作便利性
- **自動生成**：可自動生成 Collection Schema
- **範例載入**：一鍵載入範例資料和查詢
- **JSON 驗證**：自動檢查 JSON 格式
- **確認對話框**：刪除操作前的安全確認

### 📊 結果展示
- **格式化顯示**：JSON 結果自動格式化
- **顏色區分**：成功/錯誤狀態用不同顏色顯示
- **捲軸支援**：大量結果可捲動瀏覽

## 使用流程

### 典型工作流程

1. **建立 Collection**
   ```
   Collection 管理 → 建立 Collection → 輸入 Schema → 建立
   ```

2. **新增資料**
   ```
   Document 管理 → 新增/更新 Document → 輸入資料 → 新增
   ```

3. **搜尋測試**
   ```
   搜尋功能 → FTS 搜尋 → 輸入關鍵字 → 執行搜尋
   ```

4. **進階查詢**
   ```
   搜尋功能 → 進階查詢 → 輸入 DSL → 執行查詢
   ```

## 技術說明

### 前端技術
- **HTML5**：語義化標記
- **CSS3**：響應式設計和動畫效果
- **Vanilla JavaScript**：純 JavaScript，無第三方依賴
- **Fetch API**：非同步 HTTP 請求

### API 整合
- 自動處理 JSON 序列化/反序列化
- 統一的錯誤處理機制
- 非同步操作支援
- HTTP 狀態碼正確處理

### 安全性
- 輸入驗證和清理
- 刪除操作確認機制
- JSON 格式驗證
- XSS 防護

## 故障排除

### 常見問題

1. **頁面無法載入**
   - 確認服務器是否正常運行
   - 檢查 5122 端口是否被占用
   - 確認靜態檔案路徑正確

2. **API 呼叫失敗**
   - 檢查 JSON 格式是否正確
   - 確認 Collection 是否存在
   - 查看瀏覽器開發者工具的錯誤訊息

3. **搜尋無結果**
   - 確認 Collection 中有資料
   - 檢查搜尋關鍵字是否正確
   - 確認 FTS 索引是否建立成功

### 偵錯方法
- 打開瀏覽器開發者工具（F12）
- 查看 Console 標籤的錯誤訊息
- 檢查 Network 標籤的 API 請求狀態
- 參考服務器日誌檔案（`pocket_fts.log`）

## 擴展說明

前端介面設計為模組化架構，便於未來擴展：

- **新增功能**：可輕鬆添加新的 API 端點支援
- **客製化**：CSS 和 JavaScript 可獨立修改
- **國際化**：支援多語言擴展
- **主題**：可自訂介面主題和樣式

歡迎根據需求進行客製化開發！