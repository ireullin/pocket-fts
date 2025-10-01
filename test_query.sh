#!/bin/bash

set -e

FTS_DB_FILE="ef88218226408ba5c4a80b7019152f92.db"

echo "--- Starting Query API Test ---"

echo "Cleaning up previous run..."
kill $(lsof -t -i:5122) 2>/dev/null || true
sleep 1
rm -f db.sqlite db.sqlite-shm db.sqlite-wal pocket_fts.log response.body "$FTS_DB_FILE" "${FTS_DB_FILE}-journal"
echo "Cleanup complete."
echo

# Function to test query endpoint
run_query_test() {
    local test_name=$1
    local payload_file=$2
    
    echo "--- Test: $test_name ---"
    
    HTTP_STATUS=$(curl -s -o response.body -w "%{http_code}" -X POST -H "Content-Type: application/json" -d @"$payload_file" "http://localhost:5122/query")
    
    echo "Response Body:"
    cat response.body | jq . 2>/dev/null || cat response.body
    echo
    echo "HTTP Status: $HTTP_STATUS"
    
    if [ "$HTTP_STATUS" -eq "200" ]; then
        echo "Test PASSED!"
    else
        echo "Test FAILED!"
    fi
    echo
}

# Start the server in the background
echo "Starting server..."
LD_LIBRARY_PATH=./lib mise exec -- ./pocket_fts &
SERVER_PID=$!

# Give the server a moment to start up
sleep 2

# Check if the server is still running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "Server failed to start. Check pocket_fts.log for errors."
    exit 1
fi

echo "Server started with PID $SERVER_PID. Running query tests..."
echo

# Setup test data
echo "Setting up test data..."
curl -s -X POST -H "Content-Type: application/json" -d @payload_create_collection.json http://localhost:5122/collections/create > /dev/null
curl -s -X POST -H "Content-Type: application/json" -d @payload_upsert_doc.json http://localhost:5122/documents/upsert > /dev/null
echo "Test data setup complete."
echo

# Run query tests
run_query_test "Simple SQL Query" "payload_query_simple.json"
run_query_test "FTS Query" "payload_query_fts.json"
run_query_test "Complex AND/OR Query" "payload_query_complex.json"

# Clean up: kill the server process
echo "All tests completed. Shutting down server..."
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null

# Clean up temp file
rm -f response.body

echo "Query API test finished."