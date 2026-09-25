package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

func seedMeeting(t *testing.T) {
	t.Helper()
	code, body := callHandler(t, handleCollectionCreate, map[string]interface{}{
		"name":        "meetings",
		"primary_key": "id",
		"fields": []map[string]interface{}{
			{"name": "id", "type": "text"},
			{"name": "title", "type": "text", "searchable": true},
			{"name": "adopted", "type": "text"},
		},
	})
	if code != http.StatusCreated {
		t.Fatalf("collection create returned HTTP %d: %s", code, body)
	}
	code, body = callHandler(t, handleDocumentUpsert, map[string]interface{}{
		"collection": "meetings",
		"document":   map[string]interface{}{"id": "m1", "title": "週會", "adopted": ""},
	})
	if code != http.StatusOK {
		t.Fatalf("upsert returned HTTP %d: %s", code, body)
	}
}

func TestUpdateEndpointAppliesWhenConditionHolds(t *testing.T) {
	setupQueryEngine(t)
	seedMeeting(t)

	code, body := callHandler(t, handleDocumentUpdate, map[string]interface{}{
		"collection": "meetings",
		"document":   map[string]interface{}{"id": "m1", "adopted": "A"},
		"where":      [][]interface{}{{"adopted", "=", ""}},
	})
	if code != http.StatusOK {
		t.Fatalf("got HTTP %d: %s", code, body)
	}

	var title, adopted string
	if err := db.QueryRow("SELECT title, adopted FROM meetings WHERE id = 'm1'").Scan(&title, &adopted); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if title != "週會" || adopted != "A" {
		t.Fatalf("row is title=%q adopted=%q, want title unchanged and adopted=A", title, adopted)
	}
}

func TestUpdateEndpointConflictReturns409WithCurrentDocument(t *testing.T) {
	setupQueryEngine(t)
	seedMeeting(t)

	payload := map[string]interface{}{
		"collection": "meetings",
		"document":   map[string]interface{}{"id": "m1", "adopted": "A"},
		"where":      [][]interface{}{{"adopted", "=", ""}},
	}
	if code, body := callHandler(t, handleDocumentUpdate, payload); code != http.StatusOK {
		t.Fatalf("first update returned HTTP %d: %s", code, body)
	}

	payload["document"] = map[string]interface{}{"id": "m1", "adopted": "B"}
	code, body := callHandler(t, handleDocumentUpdate, payload)
	if code != http.StatusConflict {
		t.Fatalf("second update returned HTTP %d, want 409: %s", code, body)
	}

	var resp struct {
		Error   string                 `json:"error"`
		Failed  [][]interface{}        `json:"failed"`
		Current map[string]interface{} `json:"current"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("parse 409 body: %v (%s)", err, body)
	}
	if resp.Error == "" {
		t.Fatalf("409 body has no error message: %s", body)
	}
	if len(resp.Failed) != 1 || resp.Failed[0][0] != "adopted" || resp.Failed[0][1] != "=" || resp.Failed[0][2] != "" {
		t.Fatalf("failed = %v, want [[adopted = \"\"]]", resp.Failed)
	}
	if resp.Current["adopted"] != "A" || resp.Current["title"] != "週會" {
		t.Fatalf("current = %v, want the stored document with adopted=A", resp.Current)
	}
}

func TestUpdateEndpointStatusCodes(t *testing.T) {
	setupQueryEngine(t)
	seedMeeting(t)

	for _, tc := range []struct {
		name    string
		payload map[string]interface{}
		want    int
	}{
		{"missing document", map[string]interface{}{
			"collection": "meetings", "document": map[string]interface{}{"id": "nope", "adopted": "A"},
		}, http.StatusNotFound},
		{"missing collection", map[string]interface{}{
			"collection": "nope", "document": map[string]interface{}{"id": "m1"},
		}, http.StatusNotFound},
		{"unknown where field", map[string]interface{}{
			"collection": "meetings", "document": map[string]interface{}{"id": "m1", "adopted": "A"},
			"where": [][]interface{}{{"nope", "=", ""}},
		}, http.StatusBadRequest},
		{"malformed where", map[string]interface{}{
			"collection": "meetings", "document": map[string]interface{}{"id": "m1", "adopted": "A"},
			"where": [][]interface{}{{"adopted", "="}},
		}, http.StatusBadRequest},
		{"unknown document field", map[string]interface{}{
			"collection": "meetings", "document": map[string]interface{}{"id": "m1", "nope": "A"},
		}, http.StatusBadRequest},
	} {
		if code, body := callHandler(t, handleDocumentUpdate, tc.payload); code != tc.want {
			t.Errorf("%s: got HTTP %d, want %d: %s", tc.name, code, tc.want, body)
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM meetings").Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("meetings has %d rows after rejected updates, want 1", count)
	}
}
