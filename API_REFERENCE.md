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
      "has_fts": true,
      "document_count": 123
    }
  ],
  "count": 1
}
```
`has_fts` is `true` when at least one field in the schema is `indexed`. A
collection with `has_fts: false` never touches the full-text engine — see
[Full-Text Search Is Opt-In](#full-text-search-is-opt-in).

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

Full-text search is opt-in per collection: if no field has `indexed: true`,
the collection is created as a plain SQL table only and never touches the
full-text engine. See [Full-Text Search Is Opt-In](#full-text-search-is-opt-in).

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
**Response 503**
```json
{ "error": "Write timed out; the server is saturated with writes" }
```
Returned with a `Retry-After` header when the write did not complete within the
`-write-timeout` budget. See [Write Concurrency](#write-concurrency).

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
**Response 503**
```json
{ "error": "Write timed out; the server is saturated with writes" }
```
Returned with a `Retry-After` header when the write did not complete within the
`-write-timeout` budget. See [Write Concurrency](#write-concurrency).

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
| `order_by` | array\<object> | No | Sorting rules, applied in order. Defaults to `_score DESC` when a search term is present, and to no sorting otherwise. |

**SQL tuple**

| Position | Type | Description |
| --- | --- | --- |
| `0` | string | Field name. |
| `1` | string | Operator (`=`, `!=`, `>`, `>=`, `<`, `<=`, `LIKE`). |
| `2` | string/number | Value to compare; for `LIKE`, include `%` wildcards. |

**order_by object**

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `field` | string | Yes | Column used for sorting. Must be a column of the collection, or `_score`. |
| `direction` | string (`asc`\|`desc`) | Yes | Sort direction. Defaults to `asc` when omitted. |

`order_by` is validated against the collection schema. A field that is not a
column of the collection is rejected with `400`, rather than being ignored.

`_score` is a relevance rank, not an ordinary column:

- `desc` returns the most relevant rows first. `asc` returns the least relevant
  rows first.
- The underlying score is a raw ftscore value (negative on ftscore v0.13, where
  a smaller number means a better match). Do not compare it against a fixed
  threshold; use it only for ordering.
- `_score` is only produced by queries that carry a `search` clause. Using it in
  `order_by` without one is rejected with `400`.

**Request Body (flat format)**
```json
{
  "collection": "products",
  "search": {
    "term": "peach"
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

**Response 400**
```json
{ "error": "collection \"logs\" has no indexed fields; full-text search is not available" }
```
Returned when `search` is provided but the collection has no `indexed` field
(`has_fts: false` in [List Collections](#list-collections)). SQL-only queries
(no `search` clause) work on every collection regardless of `has_fts`.

---

## Root Endpoint
```
GET /
```
- Redirects to the web console when path is `/`.
- Any other path returns a plain text message confirming the service is running.

---

## Full-Text Search Is Opt-In

The full-text engine only stores what a collection asks it to index. A
collection's schema determines whether it participates:

- **At least one field has `indexed: true`.** The collection is created in
  the full-text engine, and every upsert/delete is mirrored there.
  `/query` requests with a `search` clause and `/search` both work.
- **No field has `indexed: true`.** The collection exists only as a plain
  SQL table. `/collections/create`, `/documents/upsert`, and
  `/documents/delete` never call the full-text engine. `search` clauses
  against it are rejected with `400` instead of reaching the engine.

This is fixed at creation time — there is no endpoint to change a
collection's fields afterward, so a collection's `has_fts` value never
changes over its lifetime.

## Write Concurrency

Writes are serialized. A single upsert or delete touches two SQLite databases:
the row table and the full-text index. SQLite allows one writer at a time, and
the full-text engine holds its index with a single exclusive connection, so
concurrent writes queue rather than run in parallel. Adding concurrency raises
latency without raising throughput.

Concurrent writes wait in a queue instead of competing for the database lock.
The wait is bounded by `-write-timeout` (default 30 seconds), which covers both
the queue wait and the write itself. A request that exceeds the budget returns
`503` with a `Retry-After` header. It is safe to retry.

Two consequences for callers:

- **Set a client timeout above `-write-timeout`.** Under load a write can take
  the full budget. A client that gives up sooner will disconnect on requests the
  server would have completed.
- **Check the response status on bulk ingestion.** A `503` means the document was
  not written. Scripts that ignore status codes will silently lose records.

Measured single-writer throughput depends almost entirely on storage, because
each write is bound by `fsync`: roughly 3,000 writes/second on a RAM disk,
85/second on an NVMe SSD, and 10/second on a 7200rpm hard disk. Those figures do
not improve with more concurrent writers.

> These specific numbers predate `db.sqlite` moving to WAL mode (see
> `docs/tickets/db-sqlite-wal.md`), which removes the per-commit `fsync` on
> that side of the write and measurably raises HDD throughput. The full-text
> engine's own connection is unaffected and still bounds the write as a
> whole, so the figures above have not been re-measured with the same
> sustained-throughput methodology and may now understate HDD/NVMe
> throughput. Treat them as directionally correct, not current.

## Notes

- All collection names and field identifiers must be alphanumeric or underscore (`^[a-zA-Z0-9_]+$`).
- The service returns HTTP 405 if an endpoint is called with an unsupported method.
- For best performance when using `/query`, prefer consolidating related SQL filters into the `sql` array instead of running separate requests.

Use this reference to script ingestion, maintenance, and querying workflows against the Pocket FTS API.
