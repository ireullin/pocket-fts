# Pocket FTS Web API Reference

This guide describes the HTTP endpoints exposed by the Pocket FTS server and how to interact with them. All payloads use JSON and all responses include the `Content-Type: application/json` header unless otherwise noted.

- **Base URL:** `http://{host}:{port}`
- **Authentication:** none (ensure the service is protected appropriately in production)
- **Error format:** non-`2xx` responses contain `{"error": "<message>"}`.

---

## Collections

### List Collections
```
GET /collections/list
```
**Response 200**
```json
{
  "collections": [
    {
      "name": "products",
      "primary_key": "id",
      "field_count": 4,
      "document_count": 123
    }
  ],
  "count": 1
}
```

### Create Collection
```
POST /collections/create
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Unique collection identifier (letters, numbers, underscore). |
| `primary_key` | string | Yes | Field that uniquely identifies each document. |
| `fts` | object | No | Search configuration. |
| `fields` | array\<object> | Yes | Schema definition for all fields stored in the collection. |

**fts object**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `stemming` | boolean | No | Enables word stemming so that searches match inflected forms (e.g., “run”, “running”, “ran”). Defaults to `false`. |

**Field object**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Field name. Must match identifier rules. |
| `type` | string (`text`\|`integer`\|`real`) | Yes | Storage type used in the SQL table. |
| `indexed` | boolean | No | Whether the field is indexed for text search. Defaults to `false`. |
| `weight` | number | No | Optional weighting factor for search ranking. |
| `primary_key` | boolean | No | Convenience flag; set to `true` for the primary key field. |

**Body**
```json
{
  "name": "products",
  "primary_key": "id",
  "fts": { "stemming": true },
  "fields": [
    { "name": "id", "type": "text", "indexed": true, "primary_key": true },
    { "name": "title", "type": "text", "indexed": true, "weight": 2.0 },
    { "name": "content", "type": "text", "indexed": true },
    { "name": "created_at", "type": "integer" }
  ]
}
```
**Response 201**
```json
{ "status": "success", "collection": "products" }
```

### Delete Collection
```
POST /collections/delete
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `name` | string | Yes | Collection to remove. |

**Body**
```json
{ "name": "products" }
```
**Response 200**
```json
{ "status": "success", "collection": "products" }
```

### Fetch Collection Content
```
GET /collections/content?collection={name}&page={page}&limit={limit}
```
- `collection` (required): collection name  
- `page` (optional, default `1`): 1-based page index  
- `limit` (optional, default `20`, max `100`): rows per page

| Query Parameter | Type | Required | Description |
| --- | --- | --- | --- |
| `collection` | string | Yes | Target collection. |
| `page` | integer | No | Page number (min 1). Defaults to `1`. |
| `limit` | integer | No | Page size (max 100). Defaults to `20`. |

**Response 200**
```json
{
  "collection": "products",
  "schema": "{...raw schema json...}",
  "columns": ["id", "title", "price"],
  "records": [
    { "id": "item_1", "title": "Sample", "price": 1200, "_score": 0.8123 }
  ],
  "pagination": {
    "page": 1,
    "limit": 20,
    "total_pages": 5,
    "total_count": 87
  }
}
```

---

## Documents

### Upsert Document
```
POST /documents/upsert
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `collection` | string | Yes | Collection receiving the document. |
| `document` | object | Yes | Key-value pairs forming the record. Must include the primary key field defined in the collection schema. Field value types must match the schema (`text`→string, `integer`→integer, `real`→number). |

**Body**
```json
{
  "collection": "products",
  "document": {
    "id": "item_1",
    "title": "Sample",
    "content": "Example description",
    "price": 1200
  }
}
```
**Response 200**
```json
{ "status": "success" }
```

### Delete Document
```
POST /documents/delete
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `collection` | string | Yes | Collection containing the document. |
| `id` | string | Yes | Value of the primary key for the document to delete. |

**Body**
```json
{
  "collection": "products",
  "id": "item_1"
}
```
**Response 200**
```json
{ "status": "success" }
```

---

## Search

### Full-Text Search
```
POST /search
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `collection` | string | Yes | Collection to search. |
| `query` | string | Yes | Search expression. Supports boolean keywords (`AND`, `OR`, `NOT`) and `field:value` filters. |
| `limit` | integer | No | Maximum results (defaults to engine setting). |
| `offset` | integer | No | Number of matches to skip. |
| `weights` | object | No | Adjusts search relevance per field (`fieldName`: number). |

**Body**
```json
{
  "collection": "products",
  "query": "peach AND NOT type:archive",
  "limit": 20,
  "offset": 0,
  "weights": {
    "title": 5,
    "content": 1
  }
}
```

**Response 200**
Returns the raw search results for the target collection. The exact structure matches the search engine output. Example:
```json
{
  "Hits": [
    { "ID": "item_1", "Score": 0.8123, "Fields": { "title": "White Peach Jam" } }
  ],
  "Count": 1,
  "Limit": 20,
  "Offset": 0
}
```

---

## Advanced Query

### Combined Search & SQL Filtering
```
POST /query
Content-Type: application/json
```

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `collection` | string | Yes | Collection name. |
| `search` | object | No | Full-text search clause. Omit for SQL-only queries. |
| `search.term` | string | Yes (if `search` provided) | Search expression. |
| `sql` | array\<array> | No | List of SQL filters, each entry `[field, operator, value]`. Combined with logical AND. |
| `limit` | integer | No | Maximum rows returned. |
| `offset` | integer | No | Rows to skip before returning results. |
| `order_by` | array\<object> | No | Sorting rules. Defaults to `_score DESC` when a search term is present. |

**SQL tuple**

| Position | Type | Description |
| --- | --- | --- |
| `0` | string | Field name. |
| `1` | string | Operator (`=`, `!=`, `>`, `>=`, `<`, `<=`, `LIKE`). |
| `2` | string/number | Value to compare; for `LIKE`, include `%` wildcards. |

**order_by object**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `field` | string | Yes | Column used for sorting. |
| `direction` | string (`asc`\|`desc`) | Yes | Sort direction. |

**Request Body (flat format)**
```json
{
  "collection": "products",
  "search": {
    "term": "peach AND NOT type:archive"
  },
  "sql": [
    ["price", ">", 1000],
    ["status", "=", "published"]
  ],
  "limit": 20,
  "offset": 0,
  "order_by": [
    { "field": "_score", "direction": "desc" }
  ]
}
```
- `collection` (required): collection name  
- `search.term` (optional): DSL string for full-text search. Omit to run SQL-only queries.  
- `sql` (optional): array of `[field, operator, value]` tuples, combined with logical AND. Supported operators: `"="`, `"!="`, `">"`, `">="`, `"<"`, `"<="`, `"LIKE"` (value should include wildcards, e.g. `%foo%`).  
- `limit`, `offset`, `order_by` follow SQL semantics.

**Response 200**
```json
[
  {
    "id": "item_1",
    "title": "White Peach Jam",
    "status": "published",
    "price": 1200,
    "_score": 0.8123
  }
]
```

---

## Root Endpoint
```
GET /
```
- Redirects to the web console when path is `/`.
- Any other path returns a plain text message confirming the service is running.

---

## Notes

- All collection names and field identifiers must be alphanumeric or underscore (`^[a-zA-Z0-9_]+$`).
- The service returns HTTP 405 if an endpoint is called with an unsupported method.
- For best performance when using `/query`, prefer consolidating related SQL filters into the `sql` array instead of running separate requests.

Use this reference to script ingestion, maintenance, and querying workflows against the Pocket FTS API.
