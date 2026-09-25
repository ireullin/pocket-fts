package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ireullin/pocket-fts/pocketfts"
)

// TestWriteTimeoutMapsTo503 verifies that a store write that ran out of time,
// waiting for the write lock included, reaches the client as 503 with
// Retry-After, so callers can tell "retry later" from "bad request".
func TestWriteTimeoutMapsTo503(t *testing.T) {
	rec := httptest.NewRecorder()
	respondWithWriteError(rec, fmt.Errorf("%w after 30s waiting for the write lock", pocketfts.ErrWriteTimeout))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("got HTTP %d, want 503", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("503 response has no Retry-After header")
	}
}

// TestOtherWriteErrorsMapTo500 guards the fallback: an unclassified error is
// a server error, not a timeout.
func TestOtherWriteErrorsMapTo500(t *testing.T) {
	rec := httptest.NewRecorder()
	respondWithWriteError(rec, errors.New("Failed to save document to SQL table"))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("got HTTP %d, want 500", rec.Code)
	}
}
