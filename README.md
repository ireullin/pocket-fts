# Pocket FTS

Pocket FTS 是一款輕量、易部署、對中日韓語言友善的全文檢索與資料瀏覽工具。它將資料管理與搜尋介面打包成單一可執行檔，讓你在最短時間內建立結構化資料的索引、維護集合、執行複合查詢，甚至提供最小可用的管理後台。

## 核心特色

- **輕量部署**：只需要一個 SQLite 資料檔與程式本體，沒有外部服務或複雜依賴。可在桌面、私有伺服器或容器環境即刻啟動。
- **中日韓友善**：內建適合 CJK 語言的分詞與搜尋語法支援，包含布林運算、欄位限定、語重調整等功能，能準確搜尋詞彙。
- **即時管理後台**：啟動服務後即可透過 Web 介面建立集合（Collections）、操作文件（Documents），同時檢視索引內容。
- **彈性查詢**：提供 `/search` 進行全文搜尋、`/query` 混合全文與 SQL 條件，範圍查詢、一致查詢與欄位 Like 等皆可在單一請求中完成。
- **JSON API**：所有操作皆透過 RESTful API，輕鬆整合到既有系統或自動化流程。
- **可擴充結構**：每個集合可自訂欄位型別、是否建立索引及權重設定，方便針對不同資料集調整最佳化策略。

## 使用情境

- **文件搜尋／知識庫**：快速為內部文件、FAQ、技術筆記建立索引，並透過布林語法與欄位搜尋精準定位答案。
- **開發測試工具**：在產品開發階段快速建立原型資料庫，測試搜尋策略或 REST API 互動。
- **本地資料探索**：用於個人或小型團隊，將既有 SQLite 或 JSON 資料匯入後立即檢視與搜尋。
- **離線或封閉環境**：在無法安裝大型搜尋服務的環境中提供具備全文檢索的解決方案。

## 快速開始

1. 下載或自行建置可執行檔。
2. 準備資料庫檔案（預設為 `db.sqlite`，若不存在會自動建立）。
3. 執行程式：
   ```bash
   ./pocket_fts -p 5122 -f db.sqlite -host 0.0.0.0
   ```
4. 以瀏覽器開啟 `http://localhost:5122/controller/` 使用管理後台。
5. 透過 API 建立集合與文件：
   ```bash
   curl -X POST http://localhost:5122/collections/create \
     -H "Content-Type: application/json" \
     -d '{
       "name": "products",
       "primary_key": "id",
       "fts": { "stemming": true },
       "fields": [
         { "name": "id", "type": "text", "indexed": true},
         { "name": "title", "type": "text", "indexed": true, "weight": 2.0 },
         { "name": "content", "type": "text", "indexed": true },
         { "name": "price", "type": "real" }
       ]
     }'
   ```

## API 總覽

| 功能 | 方法 | 路徑 | 說明 |
| --- | --- | --- | --- |
| 列出集合 | `GET` | `/collections/list` | 取得所有集合與相關統計。 |
| 建立集合 | `POST` | `/collections/create` | 定義欄位、索引與主鍵。 |
| 刪除集合 | `POST` | `/collections/delete` | 移除集合與相關資料。 |
| 列出集合內容 | `GET` | `/collections/content` | 以分頁方式取得文件列表。 |
| 新增/更新文件 | `POST` | `/documents/upsert` | 新增或覆寫集合中的文件。 |
| 刪除文件 | `POST` | `/documents/delete` | 依主鍵刪除文件。 |
| 全文搜尋 | `POST` | `/search` | 使用布林語法進行搜尋。 |
| 進階查詢 | `POST` | `/query` | 結合全文與 SQL 條件篩選。 |

詳細參數請參考 `docs/API_REFERENCE.md`。

## 伺服器選項

| 參數 | 預設值 | 說明 |
| --- | --- | --- |
| `-p` / `-port` | `5122` | HTTP 監聽埠。 |
| `-f` | `db.sqlite` | SQLite 資料檔路徑。 |
| `-host` | `localhost` | 綁定的主機位址。 |


## 注意事項

- 集合與欄位名稱限制為英數與底線，可避免 SQL 注入風險。
- 建議以反向代理或防火牆保護服務，只開放信任的 IP。
- 依照資料集大小調整欄位索引與權重，以兼顧效能與準確度。
- 若需備份，請一併保存 `db.sqlite` 與匯出的集合定義。

Pocket FTS 旨在提供 小而美 的全文檢索能力，無須部署複雜叢集即可建立對 CJK 語系友善的搜尋體驗。歡迎改造與整合，讓資料探索變得更輕鬆。
