package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// callGetHandler invokes a handler with a GET request and no body, mirroring
// callHandler (query_integration_test.go) which is POST-only.
func callGetHandler(t *testing.T, handler http.HandlerFunc) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Code, rec.Body.Bytes()
}

// TestNonFTSCollectionNeverTouchesFtscore verifies that creating a
// collection with zero `searchable` fields, and writing a document to it,
// never registers anything with ftscore. Before the ftscore-scope-
// separation fix, CreateCollection/UpsertDocument were called
// unconditionally, so ftscore would know about "plain" and fts.Search would
// succeed (0 hits, no error). After the fix, ftscore never hears about it,
// so fts.Search must return an error.
func TestNonFTSCollectionNeverTouchesFtscore(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "plain",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "label", "type": "text"}, // non-searchable text field; satisfies ftscore's own schema rule if it were called
			{"name": "amount", "type": "real"},
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	code, body = callHandler(t, handleDocumentUpsert, map[string]interface{}{
		"collection": "plain",
		"document": map[string]interface{}{
			"id":     "p1",
			"label":  "hello",
			"amount": 12.5,
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("document upsert returned HTTP %d: %s", code, body)
	}

	if _, err := fts.Search("plain", `{"query":"hello","limit":10}`); err == nil {
		t.Fatalf("expected fts.Search to fail for a collection ftscore was never told about, got no error")
	}
}

// TestFTSCollectionStillReachesFtscore is a regression guard: a collection
// with at least one searchable field must still behave exactly as before —
// ftscore knows about it and can search it.
func TestFTSCollectionStillReachesFtscore(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "searchable",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "title", "type": "text", "searchable": true},
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	code, body = callHandler(t, handleDocumentUpsert, map[string]interface{}{
		"collection": "searchable",
		"document":   map[string]interface{}{"id": "s1", "title": "找得到"},
	})
	if code < 200 || code > 299 {
		t.Fatalf("document upsert returned HTTP %d: %s", code, body)
	}

	resultJSON, err := fts.Search("searchable", `{"query":"找得到","limit":10}`)
	if err != nil {
		t.Fatalf("expected fts.Search to succeed for an FTS-enabled collection, got error: %v", err)
	}

	// ftscore always returns a well-formed (non-empty) envelope, even for
	// zero hits, so checking the raw JSON isn't empty proves nothing about
	// whether the document was actually indexed. Parse it and check the hit
	// itself — this is what actually proves the searchable->indexed
	// translation reached ftscore correctly, not just that the call didn't
	// error.
	var parsed struct {
		Hits []struct {
			ID string `json:"ID"`
		} `json:"Hits"`
	}
	if err := json.Unmarshal([]byte(resultJSON), &parsed); err != nil {
		t.Fatalf("failed to parse search result: %v (%s)", err, resultJSON)
	}
	if len(parsed.Hits) != 1 || parsed.Hits[0].ID != "s1" {
		t.Fatalf("expected exactly one hit with ID \"s1\", got: %s", resultJSON)
	}
}

// TestQuerySearchOnNonFTSCollectionReturns400 verifies that /query with a
// search clause against a collection that has no searchable fields fails fast
// with a clear 400, instead of reaching ftscore and surfacing its opaque
// "collection not found" error.
func TestQuerySearchOnNonFTSCollectionReturns400(t *testing.T) {
	setupQueryEngine(t)

	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "plain2",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "label", "type": "text"},
		},
	})
	if code < 200 || code > 299 {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}

	code, body = callHandler(t, handleQuery, map[string]interface{}{
		"collection": "plain2",
		"search":     map[string]string{"term": "anything"},
		"limit":      10,
	})
	if code != http.StatusBadRequest {
		t.Fatalf("expected HTTP 400 for search on a non-FTS collection, got %d: %s", code, body)
	}
}

// TestCollectionListReportsHasFTS verifies /collections/list correctly
// reports has_fts for both an FTS-enabled and a plain collection.
func TestCollectionListReportsHasFTS(t *testing.T) {
	setupQueryEngine(t)

	for _, tc := range []struct {
		name   string
		fields []map[string]interface{}
		hasFTS bool
	}{
		{
			name: "list_plain",
			fields: []map[string]interface{}{
				{"name": "id", "type": "text"},
				{"name": "label", "type": "text"},
			},
			hasFTS: false,
		},
		{
			name: "list_searchable",
			fields: []map[string]interface{}{
				{"name": "id", "type": "text"},
				{"name": "title", "type": "text", "searchable": true},
			},
			hasFTS: true,
		},
	} {
		code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
			"name":        tc.name,
			"primary_key": "id",
			"fields":      tc.fields,
		})
		if code < 200 || code > 299 {
			t.Fatalf("collection create %q returned HTTP %d: %s", tc.name, code, body)
		}
	}

	code, body := callGetHandler(t, handleCollectionList)
	if code != http.StatusOK {
		t.Fatalf("collection list returned HTTP %d: %s", code, body)
	}

	var resp struct {
		Collections []map[string]interface{} `json:"collections"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("failed to parse response: %v (%s)", err, body)
	}

	got := map[string]bool{}
	for _, c := range resp.Collections {
		name, _ := c["name"].(string)
		hasFTS, _ := c["has_fts"].(bool)
		got[name] = hasFTS
	}

	if got["list_plain"] != false {
		t.Fatalf("list_plain: has_fts = %v, want false", got["list_plain"])
	}
	if got["list_searchable"] != true {
		t.Fatalf("list_searchable: has_fts = %v, want true", got["list_searchable"])
	}
}
