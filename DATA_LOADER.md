# 資料載入程式使用說明

## 概述

`data_loader.go` 是一個專為 Pocket FTS 設計的批量資料載入工具，能夠從 TSV 檔案讀取資料並透過 Web API 載入到 FTS 引擎中。

## 功能特性

- 自動建立 `products` collection 及其 schema
- 支援 TSV 格式檔案讀取（tab 分隔）
- 自動生成隨機的價格和重量資料
- 完整的錯誤處理和進度顯示
- 自動清理舊資料避免衝突

## 資料格式

### 輸入檔案格式 (simple.tsv)

TSV 檔案應包含以下欄位（以 tab 分隔）：
```
第1欄    第2欄      第3欄（忽略）    第4欄
type    category   [ignored]        item
品項    無線耳機    [任意內容]       AK-Liberty Air 2-石英白-真無線耳機
顏色    黑色        [任意內容]       消光黑色手機殼
```

**注意：第3欄的內容會被忽略**

### 輸出資料 Schema

載入後的資料將包含以下欄位：

| 欄位名稱 | 類型 | 說明 | 範例 |
|---------|------|------|------|
| `id` | text | 自動生成的主鍵 | "item_1", "item_2" |
| `type` | text | 商品類型 | "品項", "顏色" |
| `category` | text | 商品分類 | "無線耳機", "充電器" |
| `item` | text | 商品名稱 | "AK-Liberty Air 2-石英白-真無線耳機" |
| `price` | integer | 隨機價格 | 100-50000 |
| `weight` | integer | 隨機重量（克） | 100-5000 |

## 使用方法

### 前置需求

1. **確保服務器運行中**：
```bash
cd src
LD_LIBRARY_PATH=../lib go run .
```

2. **準備 TSV 檔案**：
確保 `simple.tsv` 檔案存在於專案根目錄

### 執行載入

```bash
# 編譯程式
go build -o data_loader data_loader.go

# 執行載入
./data_loader
```

### 預期輸出

```
=== 資料載入程式 ===
清理舊的 collection...
建立 collection...
✓ Collection 建立成功
載入 TSV 資料...
已處理 50 筆資料...
已處理 100 筆資料...
...
總計處理 487 行，成功載入 487 筆資料
✓ 資料載入完成
```

## 程式架構

### 主要函數

- `main()`: 主程式流程控制
- `createCollection()`: 建立 collection 和 schema
- `deleteCollection()`: 清理舊的 collection
- `loadTSVData()`: 讀取並處理 TSV 檔案
- `upsertDocument()`: 上傳單筆文檔到 API
- `generateRandomPrice()`: 生成隨機價格
- `generateRandomWeight()`: 生成隨機重量

### 錯誤處理

程式包含完整的錯誤處理：
- HTTP 請求失敗會顯示具體錯誤
- TSV 格式錯誤會跳過該行並顯示警告
- 空欄位會被跳過並記錄
- 網路連線問題會中止程式

## 配置選項

### 修改 API 地址
```go
const BASE_URL = "http://localhost:5122"  // 修改為你的服務器地址
```

### 修改隨機數值範圍
```go
// 價格範圍 (目前: 100-50000)
func generateRandomPrice() int {
    return rand.Intn(49901) + 100
}

// 重量範圍 (目前: 100-5000克)
func generateRandomWeight() int {
    return rand.Intn(4901) + 100
}
```

### 修改 Collection Schema
在 `createCollection()` 函數中修改 `Fields` 設定：
```go
Fields: []Field{
    {Name: "id", Type: "text"},
    {Name: "type", Type: "text", Weight: 1},
    {Name: "category", Type: "text", Weight: 2},
    {Name: "item", Type: "text", Weight: 3},
    {Name: "price", Type: "integer"},
    {Name: "weight", Type: "integer"},
},
```

## 測試查詢

載入完成後，可以測試以下查詢：

### 1. FTS 搜尋
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

### 2. 複合查詢（SQL + FTS）
```bash
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "$and": [
        {
          "sql": {
            "where": {"type": "品項"}
          }
        },
        {
          "search": {
            "term": "充電"
          }
        }
      ]
    },
    "result": {"limit": 5}
  }'
```

### 3. 價格範圍查詢
```bash
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "sql": {
        "where": {
          "price": {"$gt": 30000}
        }
      }
    },
    "result": {"limit": 5}
  }'
```

## 注意事項

1. **重複執行**：程式會自動刪除舊的 `products` collection，可安全重複執行
2. **資料一致性**：確保服務器運行正常，否則可能導致部分資料載入失敗
3. **記憶體使用**：大檔案載入時注意記憶體使用情況
4. **網路逾時**：如果檔案很大，可能需要調整 HTTP 逾時設定

## 故障排除

### 常見錯誤

1. **連線被拒絕**：
   - 確認服務器是否運行在 localhost:5122
   - 檢查防火牆設定

2. **檔案不存在**：
   - 確認 `simple.tsv` 檔案存在於執行目錄
   - 檢查檔案權限

3. **格式錯誤**：
   - 確認 TSV 檔案使用 tab 分隔符
   - 檢查是否有空行或格式不正確的行

4. **記憶體不足**：
   - 分批處理大檔案
   - 增加系統記憶體或調整批次大小