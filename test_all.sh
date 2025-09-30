#!/bin/bash

set -e

# This is the MD5 hash of 'db.sqlite', which the C library uses as its internal DB file.
FTS_DB_FILE="ef88218226408ba5c4a80b7019152f92.db"

echo "--- Cleaning up previous run... ---"
echo "Attempting to free port 5122..."
kill $(lsof -t -i:5122) 2>/dev/null || true
sleep 1 # Give a moment for the port to be released

# Remove all state files, including the FTS library's own DB
rm -f db.sqlite db.sqlite-shm db.sqlite-wal pocket_fts.log response.body "$FTS_DB_FILE" "${FTS_DB_FILE}-journal"
echo "Cleanup complete."
echo

# Function to make a curl request and check the status
# Usage: expect_status <expected_http_status> <endpoint> <payload_file>
run_test() {
    local expected_status=$1
    local endpoint=$2
    local payload_file=$3
    
    echo "--- Test: POST to $endpoint ---"
    
    # Use -s for silent, -w to write out http_code
    HTTP_STATUS=$(curl -s -o response.body -w "%{http_code}" -X POST -H "Content-Type: application/json" -d @"$payload_file" "http://localhost:5122$endpoint")
    
    echo "Response Body:"
    cat response.body
    echo
    echo "Expected HTTP Status: $expected_status, Got: $HTTP_STATUS"

    if [ "$HTTP_STATUS" -ne "$expected_status" ]; then
        echo "Test FAILED!"
        exit 1
    fi
    echo "Test PASSED!"
    echo
}

# Start the server in the background
echo "Starting server..."
LD_LIBRARY_PATH=./lib mise exec -- ./pocket_fts &
SERVER_PID=$!

# Give the server a moment to start up
sleep 1

# Check if the server is still running
if ! kill -0 $SERVER_PID 2>/dev/null; then
    echo "Server failed to start. Check pocket_fts.log for errors."
    exit 1
fi

echo "Server started with PID $SERVER_PID. Running tests..."

# Run tests in sequence
run_test 201 "/collections/create" "payload_create_collection.json"
run_test 200 "/documents/upsert" "payload_upsert_doc.json"
run_test 200 "/search" "payload_search.json"
run_test 200 "/documents/delete" "payload_delete_doc.json"

# Custom test to verify deletion
echo "--- Test: Search for deleted document (should be empty) ---"
HTTP_STATUS=$(curl -s -o response.body -w "%{http_code}" -X POST -H "Content-Type: application/json" -d @payload_search.json http://localhost:5122/search)
if [ "$HTTP_STATUS" -ne "200" ]; then
    echo "Search request failed with status $HTTP_STATUS"
    exit 1
fi
# Check if the "Hits" array is empty, allowing for whitespace
if ! grep -q '"Hits"\s*:\s*\[\]' response.body; then
    echo "Test FAILED: Deleted document was found!"
    cat response.body
    exit 1
fi
echo "Response Body:"
cat response.body
echo
echo "Test PASSED: Deleted document was not found."
echo

run_test 200 "/collections/delete" "payload_delete_collection.json"

# Clean up: kill the server process
echo "All tests passed. Shutting down server..."
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null # Suppress "Terminated" message

# Clean up temp file
rm response.body

echo "Integration test finished successfully."
