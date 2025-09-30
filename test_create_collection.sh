#!/bin/bash

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

echo "Server started with PID $SERVER_PID. Sending request..."

# Send the curl request
# The -w "\nHTTP_STATUS:%{http_code}\n" will print the status code at the end
curl -s -X POST \
     -H "Content-Type: application/json" \
     -d @payload.json \
     -w "\nHTTP_STATUS:%{http_code}\n" \
     http://localhost:5122/collections

# Clean up: kill the server process
echo "Shutting down server..."
kill $SERVER_PID
wait $SERVER_PID 2>/dev/null # Suppress "Terminated" message

echo "Test finished."

