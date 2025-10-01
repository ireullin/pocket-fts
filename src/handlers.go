package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// --- Structs for API Payloads ---

type Field struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Weight  float64 `json:"weight,omitempty"`
	Indexed bool    `json:"indexed,omitempty"`
	PrimaryKey bool    `json:"primary_key,omitempty"` // Added for convenience
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

// --- Enhanced Query DSL Structures ---

type QueryRequest struct {
	Collection string      `json:"collection"`
	Query      QueryNode   `json:"query"`
	Result     ResultSpec  `json:"result,omitempty"`
}

type QueryNode struct {
	// Logical operators
	And []*QueryNode `json:"$and,omitempty"`
	Or  []*QueryNode `json:"$or,omitempty"`
	Not *QueryNode   `json:"$not,omitempty"`
	
	// Query types
	SQL    *SQLQuery    `json:"sql,omitempty"`
	Search *SearchQuery `json:"search,omitempty"`
}

type SQLQuery struct {
	Where map[string]interface{} `json:"where"`
}

type SearchQuery struct {
	Term     string             `json:"term"`
	Fields   []string           `json:"fields,omitempty"`
	Weights  map[string]float64 `json:"weights,omitempty"`
	Operator string             `json:"operator,omitempty"` // "AND" or "OR"
}

type ResultSpec struct {
	Fields  []string    `json:"fields,omitempty"`
	Limit   int         `json:"limit,omitempty"`
	Offset  int         `json:"offset,omitempty"`
	OrderBy []OrderBySpec `json:"order_by,omitempty"`
}

type OrderBySpec struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" or "desc"
}

// --- HTTP Handlers ---

func handleCollectionCreate(w http.ResponseWriter, r *http.Request) {
	logger.Info("Collection create request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for collection create", "method", r.Method, "remote_addr", r.RemoteAddr)
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
		logger.Error("Failed to unmarshal JSON for collection create", "error", err)
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if !isValidIdentifier(schema.Name) {
		logger.Warn("Invalid collection name provided", "collection_name", schema.Name, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name. Must be alphanumeric.")
		return
	}

	// 1. Create FTS collection
	if err := fts.CreateCollection(string(body)); err != nil {
		logger.Error("Failed to create FTS collection", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create FTS collection: %v", err))
		return
	}

	// 2. Save schema to our metadata table
	if err := saveCollectionSchema(schema.Name, string(body)); err != nil {
		logger.Error("Failed to save collection schema to DB", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to save collection schema: %v", err))
		return
	}

	// 3. Create the regular SQL table for storing original documents
	createTableSQL, err := generateCreateTableSQL(schema)
	if err != nil {
		logger.Error("Failed to generate CREATE TABLE SQL", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusBadRequest, fmt.Sprintf("Invalid schema for SQL table creation: %v", err))
		return
	}
	if _, err := db.Exec(createTableSQL); err != nil {
		logger.Error("Failed to create regular SQL table", "collection", schema.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to create table '%s': %v", schema.Name, err))
		return
	}

	logger.Info("Successfully created collection, FTS index, and SQL table", "collection", schema.Name)
	respondWithJSON(w, http.StatusCreated, map[string]string{"status": "success", "collection": schema.Name})
}

func handleCollectionDelete(w http.ResponseWriter, r *http.Request) {
	logger.Info("Collection delete request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for collection delete", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req CollectionDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if !isValidIdentifier(req.Name) {
		logger.Warn("Invalid collection name for delete", "collection_name", req.Name, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name.")
		return
	}

	// 1. Delete from our metadata
	if err := deleteCollectionSchema(req.Name); err != nil {
		logger.Error("Failed to delete collection schema from DB", "collection", req.Name, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete collection schema: %v", err))
		return
	}

	// 2. Delete from the FTS engine
	if err := fts.DeleteCollection(req.Name); err != nil {
		logger.Error("Inconsistency: failed to delete from FTS engine", "collection", req.Name, "error", err)
	}

	// 3. Drop the regular SQL table
	dropTableSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", req.Name) // Safe due to isValidIdentifier check
	if _, err := db.Exec(dropTableSQL); err != nil {
		logger.Error("Inconsistency: failed to drop regular SQL table", "collection", req.Name, "error", err)
	}

	logger.Info("Successfully deleted collection, FTS index, and SQL table", "collection", req.Name)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success", "collection": req.Name})
}

func handleDocumentUpsert(w http.ResponseWriter, r *http.Request) {
	logger.Info("Document upsert request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for document upsert", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req DocumentUpsertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if !isValidIdentifier(req.Collection) {
		logger.Warn("Invalid collection name for document upsert", "collection_name", req.Collection, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name.")
		return
	}

	docBytes, err := json.Marshal(req.Document)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Failed to marshal document to JSON")
		return
	}

	// 1. Upsert to FTS index
	if err := fts.UpsertDocument(req.Collection, string(docBytes)); err != nil {
		logger.Error("Failed to upsert document to FTS", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to upsert document: %v", err))
		return
	}

	// 2. Upsert to regular SQL table
	upsertSQL, values, err := generateUpsertSQL(req.Collection, req.Document)
	if err != nil {
		logger.Error("Failed to generate upsert SQL", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Could not process document for SQL upsert")
		return
	}

	if _, err := db.Exec(upsertSQL, values...); err != nil {
		logger.Error("Failed to upsert document to SQL table", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to save document to SQL table")
		return
	}

	logger.Info("Document upserted successfully", "collection", req.Collection, "remote_addr", r.RemoteAddr)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func handleDocumentDelete(w http.ResponseWriter, r *http.Request) {
	logger.Info("Document delete request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for document delete", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	var req DocumentDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if !isValidIdentifier(req.Collection) {
		logger.Warn("Invalid collection name for document delete", "collection_name", req.Collection, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name.")
		return
	}

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

	// 1. Delete from FTS index
	primaryKeyJSON := fmt.Sprintf("{\"%s\":\"%s\"}", schema.PrimaryKey, req.ID)
	if err := fts.DeleteDocument(req.Collection, primaryKeyJSON); err != nil {
		logger.Error("Failed to delete document from FTS", "collection", req.Collection, "id", req.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to delete document from FTS: %v", err))
		return
	}

	// 2. Delete from regular SQL table
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", req.Collection, schema.PrimaryKey) // Safe due to checks
	if _, err := db.Exec(deleteSQL, req.ID); err != nil {
		logger.Error("Failed to delete document from SQL table", "collection", req.Collection, "id", req.ID, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to delete document from SQL table")
		return
	}

	logger.Info("Successfully deleted document", "collection", req.Collection, "id", req.ID)
	respondWithJSON(w, http.StatusOK, map[string]string{"status": "success"})
}

func handleSearch(w http.ResponseWriter, r *http.Request) {
	logger.Info("Search request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for search", "method", r.Method, "remote_addr", r.RemoteAddr)
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
		logger.Error("Failed to perform search", "collection", req.Collection, "query", req.Query, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Search failed: %v", err))
		return
	}

	logger.Info("Search completed successfully", "collection", req.Collection, "query", req.Query, "limit", req.Limit, "offset", req.Offset, "remote_addr", r.RemoteAddr)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(resultJSON))
}

func handleQuery(w http.ResponseWriter, r *http.Request) {
	logger.Info("Enhanced query request received", "method", r.Method, "remote_addr", r.RemoteAddr)
	
	if r.Method != http.MethodPost {
		logger.Warn("Invalid method for enhanced query", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only POST method is allowed")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("Failed to read request body for query", "error", err)
		respondWithError(w, http.StatusBadRequest, "Failed to read request body")
		return
	}
	defer r.Body.Close()

	var req QueryRequest
	if err := json.Unmarshal(body, &req); err != nil {
		logger.Error("Failed to unmarshal JSON for query", "error", err)
		respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
		return
	}

	if !isValidIdentifier(req.Collection) {
		logger.Warn("Invalid collection name for query", "collection_name", req.Collection, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name")
		return
	}

	// 執行查詢
	records, err := queryExecutor.ExecuteQuery(&req)
	if err != nil {
		logger.Error("Failed to execute enhanced query", "collection", req.Collection, "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Query execution failed: %v", err))
		return
	}

	logger.Info("Enhanced query completed successfully", 
		"collection", req.Collection, 
		"result_count", len(records),
		"remote_addr", r.RemoteAddr)

	// 直接回傳SQL記錄陣列
	respondWithJSON(w, http.StatusOK, records)
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

var validIdentifierRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+`)

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	return validIdentifierRegex.MatchString(name)
}

func mapJsonTypeToSql(jsonType string) (string, error) {
	switch strings.ToLower(jsonType) {
	case "text":
		return "TEXT", nil
	case "integer":
		return "INTEGER", nil
	case "number", "real":
		return "REAL", nil
	default:
		return "", fmt.Errorf("unsupported field type: %s", jsonType)
	}
}

func generateCreateTableSQL(schema CollectionSchema) (string, error) {
	var columns []string
	for _, field := range schema.Fields {
		if !isValidIdentifier(field.Name) {
			return "", fmt.Errorf("invalid field name: %s", field.Name)
		}
		sqlType, err := mapJsonTypeToSql(field.Type)
		if err != nil {
			return "", err
		}
		columnDef := fmt.Sprintf("%s %s", field.Name, sqlType)
		if field.Name == schema.PrimaryKey {
			columnDef += " PRIMARY KEY"
		}
		columns = append(columns, columnDef)
	}

	if len(columns) == 0 {
		return "", fmt.Errorf("schema must contain at least one field")
	}

	// Safe to use Sprintf here because schema.Name is validated with isValidIdentifier
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)", schema.Name, strings.Join(columns, ", ")), nil
}

func generateUpsertSQL(collectionName string, document map[string]interface{}) (string, []interface{}, error) {
	if len(document) == 0 {
		return "", nil, fmt.Errorf("document cannot be empty")
	}

	var columns []string
	var values []interface{}
	var placeholders []string

	// Sort keys to ensure consistent order
	keys := make([]string, 0, len(document))
	for k := range document {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		if !isValidIdentifier(k) {
			return "", nil, fmt.Errorf("invalid field name in document: %s", k)
		}
		columns = append(columns, k)
		values = append(values, document[k])
		placeholders = append(placeholders, "?")
	}

	// Safe to use Sprintf here because collectionName is validated with isValidIdentifier
	sql := fmt.Sprintf("INSERT OR REPLACE INTO %s (%s) VALUES (%s)",
		collectionName,
		strings.Join(columns, ", "),
		strings.Join(placeholders, ", "),
	)

	return sql, values, nil
}
