#!/bin/bash

echo "=== Testing FTS Score Integration ==="

# Start server in background
cd src
LD_LIBRARY_PATH=../lib ./pocket_fts &
SERVER_PID=$!
sleep 2

echo "Server started with PID: $SERVER_PID"

# Test 1: Create collection
echo "Creating collection..."
curl -s -X POST http://localhost:5122/collections/create \
  -H "Content-Type: application/json" \
  -d '{
    "name": "articles",
    "primary_key": "id",
    "fts": {"stemming": true},
    "fields": [
      {"name": "id", "type": "text"},
      {"name": "title", "type": "text", "weight": 2},
      {"name": "body", "type": "text"}
    ]
  }' > /dev/null

# Test 2: Insert documents
echo "Inserting test documents..."
curl -s -X POST http://localhost:5122/documents/upsert \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "articles",
    "document": {
      "id": "1",
      "title": "Go Programming Language",
      "body": "Go is a programming language developed by Google"
    }
  }' > /dev/null

curl -s -X POST http://localhost:5122/documents/upsert \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "articles",
    "document": {
      "id": "2", 
      "title": "Python Tutorial",
      "body": "Python is another popular programming language"
    }
  }' > /dev/null

# Test 3: FTS search query with score
echo "Testing FTS search with score..."
RESPONSE=$(curl -s -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "articles",
    "query": {
      "search": {
        "term": "programming"
      }
    }
  }')

echo "FTS Query Response:"
echo "$RESPONSE" | jq '.'

# Check if _score field exists
if echo "$RESPONSE" | jq -e '.[0]._score' > /dev/null; then
    echo "✓ SUCCESS: _score field found in results"
else
    echo "✗ FAILED: _score field not found in results"
fi

# Test 4: Combined SQL + FTS query
echo "Testing combined SQL + FTS query..."
COMBINED_RESPONSE=$(curl -s -X POST http://localhost:5122/query \
  -H "Content-Type: application/json" \
  -d '{
    "collection": "articles",
    "query": {
      "$and": [
        {
          "sql": {
            "where": {
              "title": {"$contains": "Go"}
            }
          }
        },
        {
          "search": {
            "term": "programming"
          }
        }
      ]
    }
  }')

echo "Combined Query Response:"
echo "$COMBINED_RESPONSE" | jq '.'

# Cleanup
echo "Stopping server..."
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null

echo "=== Test Complete ==="