#!/bin/bash

echo "=== 資料載入測試 ==="

# 檢查 TSV 檔案是否存在
if [ ! -f "simple.tsv" ]; then
    echo "錯誤: simple.tsv 檔案不存在"
    exit 1
fi

# 檢查服務器是否運行
echo "檢查服務器狀態..."
if ! curl -s http://localhost:5122/ > /dev/null; then
    echo "錯誤: 服務器未運行，請先啟動服務器"
    exit 1
fi
echo "✓ 服務器運行中"

# 編譯並執行資料載入程式
echo "編譯資料載入程式..."
go build -o data_loader data_loader.go

echo "開始載入資料..."
./data_loader

# 測試查詢功能
echo ""
echo "=== 測試查詢功能 ==="

echo "1. 搜尋「無線耳機」："
curl -s -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "search": {
        "term": "無線耳機"
      }
    },
    "result": {
      "limit": 5
    }
  }' | jq '.[] | {id: .id, type: .type, category: .category, item: .item, price: .price, weight: .weight, score: ._score}'

echo ""
echo "2. 複合查詢：品項類型且包含「充電」："
curl -s -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "$and": [
        {
          "sql": {
            "where": {
              "type": "品項"
            }
          }
        },
        {
          "search": {
            "term": "充電"
          }
        }
      ]
    },
    "result": {
      "limit": 5
    }
  }' | jq '.[] | {id: .id, category: .category, item: .item, price: .price, score: ._score}'

echo ""
echo "3. 價格範圍查詢（價格 > 1000）："
curl -s -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "query": {
      "sql": {
        "where": {
          "price": {"$gt": 1000}
        }
      }
    },
    "result": {
      "limit": 5
    }
  }' | jq '.[] | {id: .id, item: .item, price: .price, weight: .weight}'

echo ""
echo "=== 測試完成 ==="