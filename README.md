# Pocket FTS

[中文](#pocket-fts) ｜ [English](#pocket-fts-english) ｜ [日本語](#pocket-fts日本語) ｜ [한국어](#pocket-fts한국어)

> ⚠️ 目前僅支援 Linux，其他作業系統請透過 Docker 映像執行。

Pocket FTS 是一款輕量、易部署、對中日韓語言友善的全文檢索與資料瀏覽工具。它將資料管理與搜尋介面打包成單一可執行檔，讓你在最短時間內建立結構化資料的索引、維護集合、執行複合查詢，甚至提供最小可用的管理後台。

## 核心特色

- **輕量部署**：只需要一個 SQLite 資料檔與程式本體，沒有外部服務或複雜依賴。可在桌面、私有伺服器或容器環境即刻啟動。
- **中日韓友善**：內建適合 CJK 語言的分詞與搜尋語法支援，包含布林運算、欄位限定、語重調整等功能，能準確搜尋詞彙。
- **即時管理後台**：啟動服務後即可透過 Web 介面建立集合（Collections）、操作文件（Documents），同時檢視索引內容。
- **彈性查詢**：透過 `/query` 混合全文與 SQL 條件，範圍查詢、一致查詢與欄位 Like 等皆可在單一請求中完成。
- **JSON API**：所有操作皆透過 RESTful API，輕鬆整合到既有系統或自動化流程。
- **可擴充結構**：每個集合可自訂欄位型別、是否建立索引及權重設定，方便針對不同資料集調整最佳化策略。

## 使用情境

- **文件搜尋／知識庫**：快速為內部文件、FAQ、技術筆記建立索引，並透過布林語法與欄位搜尋精準定位答案。
- **開發測試工具**：在產品開發階段快速建立原型資料庫，測試搜尋策略或 REST API 互動。
- **本地資料探索**：用於個人或小型團隊，將既有 SQLite 或 JSON 資料匯入後立即檢視與搜尋。
- **離線或封閉環境**：在無法安裝大型搜尋服務的環境中提供具備全文檢索的解決方案。

## 快速開始

1. 下載 `bin/pocket-fts` 可執行檔或自行建置。
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

### 查詢範例

```bash
# 結合搜尋與 SQL 條件的進階查詢
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "search": { "term": "peach" },
    "sql": [
      ["price", ">", 500],
      ["status", "=", "published"]
    ],
    "limit": 20,
    "offset": 0,
    "order_by": [
      { "field": "created_at", "direction": "desc" }
    ]
  }'
```

`order_by` 依序套用多個排序條件。欄位必須是該集合的欄位，或是 `_score`。

- 欄位名稱不存在時，`/query` 回傳 `400`，不會靜默忽略。
- `_score` 是相關性排名，只有帶 `search` 的查詢才會產生。`desc` 代表最相關在前。
- 未指定 `order_by` 時，帶 `search` 的查詢預設以 `_score desc` 排序，其餘查詢不排序。

### Docker 部署

1. 下載程式並確保 `bin/pocket-fts` 已存在。
2. 建立映像：
   ```bash
   docker build -t pocket-fts .
   ```
3. 以指定資料庫啟動容器（將主機上的 `db.sqlite` 掛載進容器）：
   ```bash
   docker run --rm -p 5122:5122 \
     -v $(pwd)/db.sqlite:/app/db.sqlite \
     pocket-fts -f /app/db.sqlite -p 5122 -host 0.0.0.0
   ```
4. 開啟 `http://localhost:5122/controller/` 使用後台。

## 編譯方式

1. 安裝 Go（建議 1.21 以上版本）。
2. 取得原始碼：
   ```bash
   git clone https://github.com/ireullin/pocket-fts.git
   cd pocket-fts
   ```
3. 在 Linux 環境下執行建置（請於專案根目錄）：
   ```bash
   go build -o pocket_fts ./src
   ```
   - 若在其他作業系統上建置供 Linux 使用，可加入 `GOOS=linux GOARCH=amd64` 參數。
4. 產生的 `pocket_fts` 執行檔即可依照前述步驟啟動。

## API 總覽

| 功能 | 方法 | 路徑 | 說明 |
| --- | --- | --- | --- |
| 列出集合 | `GET` | `/collections/list` | 取得所有集合與相關統計。 |
| 建立集合 | `POST` | `/collections/create` | 定義欄位、索引與主鍵。 |
| 刪除集合 | `POST` | `/collections/delete` | 移除集合與相關資料。 |
| 列出集合內容 | `GET` | `/collections/content` | 以分頁方式取得文件列表。 |
| 新增/更新文件 | `POST` | `/documents/upsert` | 新增或覆寫集合中的文件。 |
| 刪除文件 | `POST` | `/documents/delete` | 依主鍵刪除文件。 |
| 進階查詢 | `POST` | `/query` | 結合全文與 SQL 條件篩選。 |

詳細參數請參照 [API_REFERENCE.md](API_REFERENCE.md)。

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

---

## Pocket FTS (English)

> ⚠️ Linux only at the moment. Please run the Docker image on other operating systems.

Pocket FTS is a lightweight, easy-to-deploy search and data browser designed with CJK language friendliness in mind. Everything ships as a single binary, so you can build indexes, manage collections, run combined queries, and use a minimal admin console within minutes.

### Highlights

- **Minimal footprint:** Only a SQLite file and the executable are required—no external services or heavy dependencies. Perfect for desktops, private servers, or containers.
- **CJK-aware search:** Supports boolean operators, field scoping, and weighting tuned for Chinese, Japanese, and Korean vocabularies.
- **Instant web console:** Launch the service and immediately manage collections and documents through the built-in UI.
- **Flexible querying:** Use `/query` to blend full-text lookups with SQL filters in a single request.
- **Clean JSON API:** Every action is exposed via REST, making integration with automations and existing systems straightforward.
- **Configurable schema:** Define field types, index options, and weights per collection to match your dataset’s needs.

### Use Cases

- **Document/knowledge search:** Index internal docs, FAQs, and technical notes, then pinpoint answers with boolean and field queries.
- **Dev/test companion:** Spin up a quick prototype datastore to validate search strategies or API interactions during development.
- **Local data exploration:** Import existing SQLite or JSON data for personal or small-team browsing and querying.
- **Offline or restricted environments:** Drop-in full-text search where large-scale services are impractical.

### Quick Start

1. Download `bin/pocket-fts` (prebuilt) or build from source.
2. Prepare the database file (`db.sqlite` by default; created automatically if missing).
3. Launch:
   ```bash
   ./pocket_fts -p 5122 -f db.sqlite -host 0.0.0.0
   ```
4. Open `http://localhost:5122/controller/` in your browser to access the admin console.
5. Create a collection and insert documents via the API:
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

### Query Examples

```bash
# Advanced query combining search and SQL filters
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "search": { "term": "peach" },
    "sql": [
      ["price", ">", 500],
      ["status", "=", "published"]
    ],
    "limit": 20,
    "offset": 0,
    "order_by": [
      { "field": "created_at", "direction": "desc" }
    ]
  }'
```

`order_by` applies each rule in turn. A field must be a column of the collection, or `_score`.

- An unknown field name returns `400` from `/query`. It is never ignored silently.
- `_score` is a relevance rank. Only a query with a `search` clause produces it. `desc` returns the most relevant rows first.
- Without `order_by`, a query with `search` defaults to `_score desc`. Other queries are not sorted.

### Docker Usage

1. Ensure the repository is cloned and `bin/pocket-fts` is present.
2. Build the image:
   ```bash
   docker build -t pocket-fts .
   ```
3. Run the container (mount your database file):
   ```bash
   docker run --rm -p 5122:5122 \
     -v $(pwd)/db.sqlite:/app/db.sqlite \
     pocket-fts -f /app/db.sqlite -p 5122 -host 0.0.0.0
   ```
4. Access the admin console at `http://localhost:5122/controller/`.

### Build from Source

1. Install Go (recommended 1.21 or newer).
2. Clone this repository:
   ```bash
   git clone https://github.com/ireullin/pocket-fts.git
   cd pocket-fts
   ```
3. Build the binary on Linux (run from the project root):
   ```bash
   go build -o pocket_fts ./src
   ```
   - To cross-compile on another OS: `GOOS=linux GOARCH=amd64 go build -o pocket_fts ./src`
4. Run the resulting `pocket_fts` binary following the quick-start steps above.


### API Overview

| Capability | Method | Path | Description |
| --- | --- | --- | --- |
| List collections | `GET` | `/collections/list` | Fetch collections and stats. |
| Create collection | `POST` | `/collections/create` | Define fields, indexes, and primary key. |
| Delete collection | `POST` | `/collections/delete` | Remove a collection and its data. |
| Browse collection | `GET` | `/collections/content` | Page through documents. |
| Upsert document | `POST` | `/documents/upsert` | Insert or overwrite a record. |
| Delete document | `POST` | `/documents/delete` | Remove a record by primary key. |
| Advanced query | `POST` | `/query` | Combine search with SQL filtering. |

See [API_REFERENCE.md](API_REFERENCE.md) for in-depth usage details.

### Server Options

| Flag | Default | Purpose |
| --- | --- | --- |
| `-p` / `-port` | `5122` | HTTP listen port. |
| `-f` | `db.sqlite` | SQLite database path. |
| `-host` | `localhost` | Bind address. |
| `-startup-only` | `false` | Health-check mode; exit after initialization. |

### Notes

- Collection and field names accept only alphanumerics and underscores, which helps avoid SQL injection.
- Protect the service behind a reverse proxy or firewall—expose it only to trusted networks.
- Tune indexes and weights per dataset to balance precision and performance.
- Back up both `db.sqlite` and exported collection definitions.

Pocket FTS delivers a compact yet capable search experience. No complex cluster required—spin up a CJK-friendly search environment anywhere.

---

## Pocket FTS（日本語）

> ⚠️ 現在は Linux のみ対応しています。その他の OS では Docker コンテナをご利用ください。

Pocket FTS は、軽量・簡単デプロイ・CJK（中国語・日本語・韓国語）に優しい全文検索＆データブラウジングツールです。データ管理と検索 UI を単一バイナリにまとめ、最小限のセットアップで構造化データのインデックス構築、コレクション管理、複合クエリの実行、管理コンソールの利用を可能にします。

### 主な特徴

- **軽量デプロイ**：必要なのは SQLite データファイルと実行ファイルのみ。外部サービスや複雑な依存関係は不要で、デスクトップ・社内サーバー・コンテナ環境で即座に起動できます。
- **CJK 対応**：CJK 向けの分かち書きと検索構文をサポートし、ブール演算・フィールド指定・重み付けなどで「蜜桃」「日本語」などの語句も正確にヒットさせられます。
- **リアルタイム管理コンソール**：起動するだけで Web コンソールからコレクションの作成、ドキュメント操作、インデックス内容の確認が可能です。
- **柔軟なクエリ**：`/query` を使って全文検索と SQL 条件を 1 リクエストで組み合わせられます。
- **JSON API**：すべての操作が RESTful API 経由で行え、既存のシステムや自動化パイプラインに容易に組み込めます。
- **拡張性のあるスキーマ**：コレクションごとにフィールド型、インデックス設定、重み付けを定義でき、データセットに合わせた最適化が可能です。

### 利用シナリオ

- **ドキュメント検索／ナレッジベース**：社内資料や FAQ、技術メモを素早くインデックス化し、ブール検索とフィールド検索で正確に回答を探せます。
- **開発・テストツール**：プロトタイプ開発時に手軽なテスト用データベースとして活用し、検索戦略や REST API の挙動を検証できます。
- **ローカルデータ探索**：個人または小規模チーム向けに、既存の SQLite／JSON データを取り込んで即座に閲覧・検索できます。
- **オフライン／閉域環境**：大規模な検索サービスを導入できない環境でも、全文検索機能を素早く提供できます。

### クイックスタート

1. `bin/pocket-fts` をダウンロードするか、自身でビルドします。
2. データベースファイル（既定は `db.sqlite`。存在しない場合は自動生成）を用意します。
3. 以下で起動します：
   ```bash
   ./pocket_fts -p 5122 -f db.sqlite -host 0.0.0.0
   ```
4. ブラウザで `http://localhost:5122/controller/` を開き、管理コンソールを利用します。
5. API でコレクションおよびドキュメントを登録します：
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

### クエリ例

```bash
# 全文検索と SQL 条件を組み合わせた高度なクエリ
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "search": { "term": "peach" },
    "sql": [
      ["price", ">", 500],
      ["status", "=", "published"]
    ],
    "limit": 20,
    "offset": 0,
    "order_by": [
      { "field": "created_at", "direction": "desc" }
    ]
  }'
```

`order_by` は指定した順に並べ替え条件を適用します。フィールドはコレクションの列、または `_score` である必要があります。

- 存在しないフィールド名を指定すると、`/query` は `400` を返します。無視されることはありません。
- `_score` は関連度の順位です。`search` 句を含むクエリだけが生成します。`desc` は関連度が最も高い行を先頭に返します。
- `order_by` を省略した場合、`search` を含むクエリは `_score desc` で並べ替えます。その他のクエリは並べ替えません。

### Docker での利用

1. リポジトリを取得し、`bin/pocket-fts` が存在することを確認します。
2. イメージをビルドします。
   ```bash
   docker build -t pocket-fts .
   ```
3. 必要なデータベースをマウントしてコンテナを起動します。
   ```bash
   docker run --rm -p 5122:5122 \
     -v $(pwd)/db.sqlite:/app/db.sqlite \
     pocket-fts -f /app/db.sqlite -p 5122 -host 0.0.0.0
   ```
4. `http://localhost:5122/controller/` で管理 UI を開きます。

### ビルド手順

1. Go（推奨 1.21 以上）をインストールします。
2. リポジトリを取得します：
   ```bash
   git clone https://github.com/ireullin/pocket-fts.git
   cd pocket-fts
   ```
3. Linux 環境でビルドします（プロジェクトのルートで実行）：
   ```bash
   go build -o pocket_fts ./src
   ```
   - 他 OS から Linux 向けにクロスビルドする場合は `GOOS=linux GOARCH=amd64 go build -o pocket_fts ./src` を利用してください。
4. 生成された `pocket_fts` バイナリを上記の手順で起動します。


### API 一覧

| 機能 | メソッド | パス | 説明 |
| --- | --- | --- | --- |
| コレクション一覧 | `GET` | `/collections/list` | コレクションと統計情報を取得 |
| コレクション作成 | `POST` | `/collections/create` | フィールド・インデックス・主キーを定義 |
| コレクション削除 | `POST` | `/collections/delete` | コレクションとそのデータを削除 |
| コレクション内容取得 | `GET` | `/collections/content` | ページングされたドキュメント一覧 |
| ドキュメント追加／更新 | `POST` | `/documents/upsert` | ドキュメントの新規作成・上書き |
| ドキュメント削除 | `POST` | `/documents/delete` | 主キーでドキュメント削除 |
| 高度なクエリ | `POST` | `/query` | 全文検索と SQL 条件の組み合わせ |

詳細な使い方は [API_REFERENCE.md](API_REFERENCE.md) を参照してください。

### サーバーオプション

| オプション | 既定値 | 説明 |
| --- | --- | --- |
| `-p` / `-port` | `5122` | HTTP リッスンポート |
| `-f` | `db.sqlite` | SQLite データファイルのパス |
| `-host` | `localhost` | バインドするホストアドレス |
| `-startup-only` | `false` | 起動確認のみ行い即終了 |

### 注意事項

- コレクション名とフィールド名は英数字およびアンダースコアのみ使用でき、SQL インジェクション対策になります。
- 逆プロキシやファイアウォールで信頼済み IP のみに公開することを推奨します。
- データセットに合わせてインデックスや重みを調整し、精度と性能のバランスを最適化してください。
- バックアップ時は `db.sqlite` とコレクション定義（JSON）を併せて保存してください。

Pocket FTS はコンパクトで実用的な全文検索体験を提供します。複雑なクラスター構築なしで、CJK に対応した検索環境を素早く構築可能です。

---

## Pocket FTS（한국어）

> ⚠️ 현재는 Linux만 지원하며, 다른 운영체제에서는 Docker 컨테이너로 실행해 주세요.

Pocket FTS는 가볍고 배포가 쉬우며, 중국어·일본어·한국어(CJK)에 친화적인全文 검색 및 데이터 탐색 도구입니다. 데이터 관리와 검색 인터페이스를 단일 실행 파일로 통합하여, 구조화된 데이터를 신속히 인덱싱하고 컬렉션을 관리하며, 복합 쿼리를 실행할 수 있는 최소한의 관리 콘솔을 제공합니다.

### 주요 특징

- **경량 배포**: SQLite 데이터 파일과 실행 파일만 있으면 됩니다. 외부 서비스나 복잡한 의존성이 없어 데스크톱, 사내 서버, 컨테이너 환경 어디서나 즉시 실행할 수 있습니다.
- **CJK 친화성**: CJK 언어에 적합한 토큰화와 검색 문법을 지원하며, 불리언 연산·필드 제한·가중치 등을 통해 “蜜桃”, “한글” 같은 어휘도 정확하게 찾을 수 있습니다.
- **실시간 관리 콘솔**: 서비스를 시작하면 웹 콘솔에서 컬렉션 생성, 문서 관리, 인덱스 확인을 즉시 수행할 수 있습니다.
- **유연한 쿼리**: `/query`로全文 검색과 SQL 조건을 결합하여 한 번의 요청으로 복합 필터링을 처리합니다.
- **JSON API**: 모든 작업이 RESTful API로 노출되어 기존 시스템 및 자동화 파이프라인에 손쉽게 통합할 수 있습니다.
- **확장 가능한 스키마**: 컬렉션마다 필드 타입, 인덱스 여부, 가중치를 정의해 데이터셋에 맞는 최적화를 할 수 있습니다.

### 활용 사례

- **문서 검색/지식 베이스**: 사내 문서, FAQ, 기술 메모 등을 빠르게 인덱싱하고, 불리언·필드 검색으로 정확한 답을 찾습니다.
- **개발·테스트 도구**: 제품 개발 단계에서 프로토타입 데이터베이스로 활용하여 검색 전략이나 REST API 상호작용을 검증합니다.
- **로컬 데이터 탐색**: 개인 또는 소규모 팀이 기존 SQLite/JSON 데이터를 불러와 즉시 조회·검색할 수 있습니다.
- **오프라인/폐쇄망 환경**: 대형 검색 서비스를 설치하기 어려운 환경에서도全文 검색 기능을 빠르게 제공할 수 있습니다.

### 빠른 시작

1. `bin/pocket-fts` 실행 파일을 다운로드하거나 직접 빌드합니다.
2. 데이터베이스 파일을 준비합니다(기본값 `db.sqlite`, 없으면 자동 생성).
3. 다음 명령으로 실행합니다.
   ```bash
   ./pocket_fts -p 5122 -f db.sqlite -host 0.0.0.0
   ```
4. 브라우저에서 `http://localhost:5122/controller/` 를 열어 관리 콘솔을 사용합니다.
5. API 로 컬렉션과 문서를 등록합니다.
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

### 쿼리 예시

```bash
#全文 검색과 SQL 조건을 결합한 고급 쿼리
curl -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "products",
    "search": { "term": "peach" },
    "sql": [
      ["price", ">", 500],
      ["status", "=", "published"]
    ],
    "limit": 20,
    "offset": 0,
    "order_by": [
      { "field": "created_at", "direction": "desc" }
    ]
  }'
```

`order_by`는 지정한 순서대로 정렬 규칙을 적용합니다. 필드는 컬렉션의 열이거나 `_score`여야 합니다.

- 존재하지 않는 필드 이름을 지정하면 `/query`가 `400`을 반환합니다. 조용히 무시하지 않습니다.
- `_score`는 관련도 순위입니다. `search` 절이 있는 쿼리만 생성합니다. `desc`는 관련도가 가장 높은 행을 먼저 반환합니다.
- `order_by`를 생략하면 `search`가 있는 쿼리는 `_score desc`로 정렬합니다. 그 외 쿼리는 정렬하지 않습니다.

### Docker 사용 방법

1. 리포지토리를 내려받고 `bin/pocket-fts` 존재 여부를 확인합니다.
2. 이미지 빌드:
   ```bash
   docker build -t pocket-fts .
   ```
3. 데이터베이스 파일을 마운트하여 컨테이너 실행:
   ```bash
   docker run --rm -p 5122:5122 \
     -v $(pwd)/db.sqlite:/app/db.sqlite \
     pocket-fts -f /app/db.sqlite -p 5122 -host 0.0.0.0
   ```
4. `http://localhost:5122/controller/` 에 접속해 관리 콘솔을 사용합니다.

### 빌드 방법

1. Go(권장 1.21 이상)을 설치합니다.
2. 소스를 가져옵니다.
   ```bash
   git clone https://github.com/ireullin/pocket-fts.git
   cd pocket-fts
   ```
3. Linux 환경에서 빌드합니다(프로젝트 루트에서 실행).
   ```bash
   go build -o pocket_fts ./src
   ```
   - 다른 OS에서 Linux용으로 크로스 빌드하려면 `GOOS=linux GOARCH=amd64 go build -o pocket_fts ./src` 를 사용하세요.
4. 생성된 `pocket_fts` 바이너리를 앞서 소개한 방법으로 실행합니다.

### API 개요

| 기능 | 메서드 | 경로 | 설명 |
| --- | --- | --- | --- |
| 컬렉션 목록 | `GET` | `/collections/list` | 모든 컬렉션과 통계를 조회 |
| 컬렉션 생성 | `POST` | `/collections/create` | 필드, 인덱스, 기본 키 정의 |
| 컬렉션 삭제 | `POST` | `/collections/delete` | 컬렉션 및 데이터를 삭제 |
| 컬렉션 내용 조회 | `GET` | `/collections/content` | 페이지네이션된 문서 목록 |
| 문서 추가/갱신 | `POST` | `/documents/upsert` | 문서를 새로 추가하거나 덮어쓰기 |
| 문서 삭제 | `POST` | `/documents/delete` | 기본 키로 문서를 삭제 |
| 고급 쿼리 | `POST` | `/query` |全文과 SQL 조건을 결합 |

자세한 사용 방법은 [API_REFERENCE.md](API_REFERENCE.md) 를 참조하세요.

### 서버 옵션

| 옵션 | 기본값 | 설명 |
| --- | --- | --- |
| `-p` / `-port` | `5122` | HTTP 리슨 포트 |
| `-f` | `db.sqlite` | SQLite 데이터 파일 경로 |
| `-host` | `localhost` | 바인딩할 호스트 주소 |
| `-startup-only` | `false` | 기동 테스트용, 곧바로 종료 |

### 유의 사항

- 컬렉션/필드 이름은 영숫자와 밑줄만 허용되어 SQL 인젝션 위험을 줄입니다.
- 리버스 프록시나 방화벽을 이용해 신뢰할 수 있는 IP에만 서비스 공개를 권장합니다.
- 데이터셋에 따라 인덱스와 가중치를 조정하여 정확도와 성능을 균형 있게 유지하세요.
- 백업 시에는 `db.sqlite` 와 내보낸 컬렉션 정의를 함께 보관하십시오.

Pocket FTS는 작고도 실용적인全文 검색 경험을 제공합니다. 복잡한 클러스터 구성 없이도 CJK 언어에 친화적인 검색 환경을 빠르게 구축할 수 있습니다.
