package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// --- Structs for API Payloads ---

type Field struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Weight  float64 `json:"weight,omitempty"`
	Indexed bool    `json:"indexed,omitempty"`
}

type FTSConfig struct {
	Stemming bool `json:"stemming"`
}

type CollectionSchema struct {
	Name       string    `json:"name"`
	PrimaryKey string    `json:"primary_key"`
	FTS        FTSConfig `json:"fts"`
	Fields     []Field   `json:"fields"`
}

type CollectionDeleteRequest struct {
	Name string `json:"name"`
}

type DocumentUpsertRequest struct {
	Collection string                 `json:"collection"`
	Document   map[string]interface{} `json:"document"`
}

type DocumentDeleteRequest struct {
	Collection string `json:"collection"`
	ID         string `json:"id"`
}

type SearchRequest struct {
	Collection string                 `json:"collection"`
	Query      string                 `json:"query"`
	Limit      int                    `json:"limit,omitempty"`
	Offset     int                    `json:"offset,omitempty"`
	Weights    map[string]interface{} `json:"weights,omitempty"`
}

// --- HTTP Handlers ---

func handleCollectionCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Failed to read request body", "error", err)
		respondWithError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var schema CollectionSchema
	if err := json.Unmarshal(body, &schema); err != nil {
		logger.Error("Failed to unmarshal JSON", "error", err)
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if schema.Name == "" {
		respondWithError(w, http.StatusBadRequest, "Collection name is required")
		return
	}

	if err := fts.CreateCollection(string(body)); err != nil {
		logger.Error("Failed to create FTS collection", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create FTS collection: %v", err))
		return
	}

	if err := saveCollectionSchema(schema.Name, string(body)); err != nil {
		logger.Error("Failed to save collection schema to DB", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save collection schema: %v", err))
		return
	}

	logger.Info("Successfully created collection", "collection", schema.Name)
	respondWithJSON(w, http.StatusCreated, map[string]string{"status": "success", "collection": schema.Name})
}

func handleCollectionDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req CollectionDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if req.Name == "" {
		respondWithError(w, http.StatusBadRequest, "Collection name is required")
		return
	}

	// First, delete from our own DB
	if err := deleteCollectionSchema(req.Name); err != nil {
		logger.Error("Failed to delete collection schema from DB", "collection", req.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete collection schema: %v", err))
		return
	}

	// Second, delete from the FTS engine
	if err := fts.DeleteCollection(req.Name); err != nil {
		// Log the inconsistency but don't fail the request, as our DB state is clean.
		logger.Error("Inconsistency: collection schema deleted from DB, but failed to delete from FTS engine", "collection", req.Name, "error", err)
	}

	logger.Info("Successfully deleted collection", "collection", req.Name)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success", "collection": req.Name})
}

func handleDocumentUpsert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req DocumentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	docBytes, err := json.Marshal(req.Document)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to marshal document to JSON")
		return
	}

	if err := fts.UpsertDocument(req.Collection, string(docBytes)); err != nil {
		logger.Error("Failed to upsert document", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to upsert document: %v", err))
		return
	}

	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func handleDocumentDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req DocumentDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	// Get the primary key field name from the stored schema
	schemaString, err := getCollectionSchema(req.Collection)
	if err != nil {
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("Collection '%s' not found", req.Collection))
		return
	}

	var schema CollectionSchema
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		respondWithError(w, http.StatusInternalServerError, "Failed to parse stored collection schema")
		return
	}

	// Construct the primary key JSON for the C library
	primaryKeyJSON := fmt.Sprintf("{\"%s\":\"%s\"}", schema.PrimaryKey, req.ID)

	if err := fts.DeleteDocument(req.Collection, primaryKeyJSON); err != nil {
		logger.Error("Failed to delete document", "collection", req.Collection, "id", req.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete document: %v", err))
		return
	}

	logger.Info("Successfully deleted document", "collection", req.Collection, "id", req.ID)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var req SearchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format for search request")
		return
	}

	resultJSON, err := fts.Search(req.Collection, string(body))
	if err != nil {
		logger.Error("Failed to perform search", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Search failed: %v", err))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(resultJSON))
}

// --- Helper Functions ---

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
