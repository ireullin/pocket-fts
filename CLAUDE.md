# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Pocket FTS is a full-text search engine written in Go that provides HTTP API endpoints for document indexing and searching. It combines a custom C library (`libftscore`) for FTS operations with SQLite for relational data storage.

**Key Features:**
- Full-text search with BM25 scoring and stemming support
- SQL-style queries with complex operators
- Hybrid query DSL combining SQL and FTS searches
- FTS score integration with SQL results
- Real-time logging and version tracking
- Batch data loading from TSV files

## Architecture

### Core Components

- **HTTP Server** (`src/main.go`): Entry point with flag parsing, server initialization, and route registration
- **Database Layer** (`src/database.go`): SQLite operations for collection metadata and document storage using `modernc.org/sqlite`
- **FTS Wrapper** (`src/fts_wrapper.go`): CGO bindings to the C library (`lib/libftscore.so`)
- **HTTP Handlers** (`src/handlers.go`): REST API endpoints for collection/document operations
- **Query Engine** (`src/query.go`): Advanced query execution with JSON DSL support, FTS score integration
- **Data Loader** (`data_loader.go`): Batch loading utility for TSV files with random data generation

### Data Flow

1. Collections are created with JSON schemas defining fields, types, and FTS configuration
2. Documents are stored in both SQLite tables (for structured queries) and the C FTS library (for search)
3. The system maintains schema metadata in a `collections` table
4. Search requests are proxied to the C library and results returned as JSON

### C Library Integration

The project uses CGO to integrate with `libftscore.so`. Key functions:
- `FtsEngineNew`: Initialize FTS engine with database path and stemming configuration
- `FtsCreateCollection/DeleteCollection`: Manage FTS collections
- `FtsUpsertDocument/DeleteDocument`: Document operations
- `FtsSearch`: Execute search queries

## Development Commands

### Building
```bash
# Build from source directory
cd src
go build -o pocket_fts

# Or run directly without building
LD_LIBRARY_PATH=../lib go run .
```

### Running
```bash
# Start server (default port 5122, database db.sqlite)
cd src
LD_LIBRARY_PATH=../lib ./pocket_fts

# With custom parameters
LD_LIBRARY_PATH=../lib ./pocket_fts -p 8080 -f custom.db

# Startup-only mode (initializes then exits)
LD_LIBRARY_PATH=../lib ./pocket_fts -startup-only

# Run directly from source (recommended for development)
LD_LIBRARY_PATH=../lib go run .
```

### Data Loading
```bash
# Compile and run the data loader
go build -o data_loader data_loader.go
./data_loader

# The data loader will:
# 1. Delete existing 'products' collection
# 2. Create new collection with predefined schema
# 3. Load data from simple.tsv (ignoring 3rd column)
# 4. Generate random price (100-50000) and weight (100-5000g)
```

### Testing
```bash
# Run full integration test suite
./test_all.sh

# Test single collection creation
./test_create_collection.sh
```

The test suite:
1. Cleans up previous state (kills processes, removes DB files)
2. Starts the server in background
3. Runs API tests using curl with JSON payloads
4. Verifies responses and HTTP status codes
5. Tests document lifecycle (create → upsert → search → delete)

## API Endpoints

- `POST /collections/create` - Create collection with schema
- `POST /collections/delete` - Delete collection
- `POST /documents/upsert` - Insert/update document
- `POST /documents/delete` - Delete document by ID
- `POST /search` - Search documents (simple FTS)
- `POST /query` - Enhanced query with JSON DSL (SQL + FTS + logical operators)

All endpoints expect JSON payloads. See `payload_*.json` files for examples.

### Enhanced Query DSL

The `/query` endpoint supports a powerful JSON DSL for complex queries:

```json
{
  "collection": "articles",
  "query": {
    "$and": [
      {
        "sql": {
          "where": {"status": "published"}
        }
      },
      {
        "$or": [
          {
            "search": {
              "term": "Go programming",
              "fields": ["title", "body"],
              "weights": {"title": 3, "body": 1}
            }
          },
          {
            "sql": {
              "where": {"category": "tech"}
            }
          }
        ]
      }
    ]
  },
  "result": {
    "fields": ["id", "title", "body"],
    "limit": 10,
    "order_by": [{"field": "created_at", "direction": "desc"}]
  }
}
```

**Supported operators:**
- Logical: `$and`, `$or`, `$not`
- SQL operators: `$eq`, `$ne`, `$gt`, `$gte`, `$lt`, `$lte`, `$in`, `$nin`, `$like`, `$contains`, `$null`, `$not_null`
- Query types: `sql` (relational queries), `search` (full-text search)

**Execution strategy:** 
- SQL and FTS queries are executed separately
- Results are combined using set operations (intersection, union, difference)
- FTS scores are preserved and merged with SQL results
- Final records are sorted by FTS score (lower scores = more relevant)
- Results include `_score` field when FTS queries are involved

## Supported Data Types

The system supports the following field types in collection schemas:
- `"text"` → SQL `TEXT` (for strings and searchable content)
- `"integer"` → SQL `INTEGER` (for whole numbers)
- `"number"` or `"real"` → SQL `REAL` (for floating-point numbers)

## Version Information

The system logs the FTS core version on startup. Current version can be retrieved via:
- Server logs: Look for "FTS core version" messages
- Function: `GetFTSVersion()` in `fts_wrapper.go`

## Important Notes

- The C library creates its own database file (MD5 hash of main DB path)
- All SQL identifiers are validated with regex to prevent injection
- Both SQLite and FTS operations must succeed for consistency
- Server logs to `pocket_fts.log` in JSON format with timestamp and level
- The `LD_LIBRARY_PATH` must include `./lib` to load the shared library
- FTS searches return scores where lower values indicate higher relevance