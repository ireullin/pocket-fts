package main

import (
	"database/sql"
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
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Weight     float64 `json:"weight,omitempty"`
	Indexed    bool    `json:"indexed,omitempty"`
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
	Collection string     `json:"collection"`
	Query      QueryNode  `json:"query"`
	Result     ResultSpec `json:"result,omitempty"`
}

type FlatQueryRequest struct {
	Collection string          `json:"collection"`
	Search     *FlatSearchSpec `json:"search,omitempty"`
	SQL        [][]interface{} `json:"sql,omitempty"`
	Limit      int             `json:"limit,omitempty"`
	Offset     int             `json:"offset,omitempty"`
	OrderBy    []OrderBySpec   `json:"order_by,omitempty"`
}

type FlatSearchSpec struct {
	Term string `json:"term"`
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
	Fields  []string      `json:"fields,omitempty"`
	Limit   int           `json:"limit,omitempty"`
	Offset  int           `json:"offset,omitempty"`
	OrderBy []OrderBySpec `json:"order_by,omitempty"`
}

type OrderBySpec struct {
	Field     string `json:"field"`
	Direction string `json:"direction"` // "asc" or "desc"
}

func (f *FlatQueryRequest) hasNewFormatFields() bool {
	if f == nil {
		return false
	}
	if f.Search != nil {
		return true
	}
	if f.SQL != nil {
		return true
	}
	if f.OrderBy != nil {
		return true
	}
	if f.Limit != 0 || f.Offset != 0 {
		return true
	}
	return false
}

func (f *FlatQueryRequest) toQueryRequest() (*QueryRequest, error) {
	if f == nil {
		return nil, fmt.Errorf("invalid request")
	}

	if strings.TrimSpace(f.Collection) == "" {
		return nil, fmt.Errorf("collection is required")
	}

	var nodes []*QueryNode
	hasSearch := false

	if f.Search != nil {
		term := strings.TrimSpace(f.Search.Term)
		if term != "" {
			nodes = append(nodes, &QueryNode{
				Search: &SearchQuery{Term: term},
			})
			hasSearch = true
		}
	}

	if len(f.SQL) > 0 {
		var clauses []map[string]interface{}

		for _, condition := range f.SQL {
			if len(condition) < 3 {
				return nil, fmt.Errorf("invalid sql condition: expected [field, operator, value]")
			}

			field, ok := condition[0].(string)
			if !ok || strings.TrimSpace(field) == "" {
				return nil, fmt.Errorf("invalid sql condition field")
			}

			operatorRaw, ok := condition[1].(string)
			if !ok || strings.TrimSpace(operatorRaw) == "" {
				return nil, fmt.Errorf("invalid sql condition operator")
			}

			mappedOp, err := mapSQLOperator(operatorRaw)
			if err != nil {
				return nil, err
			}

			var value interface{}
			if len(condition) > 2 {
				value = condition[2]
			}

			clause := map[string]interface{}{}
			if mappedOp == "$eq" {
				clause[field] = value
			} else {
				clause[field] = map[string]interface{}{mappedOp: value}
			}

			clauses = append(clauses, clause)
		}

		var where map[string]interface{}
		if len(clauses) == 1 {
			where = clauses[0]
		} else {
			andList := make([]interface{}, 0, len(clauses))
			for _, clause := range clauses {
				andList = append(andList, clause)
			}
			where = map[string]interface{}{"$and": andList}
		}

		nodes = append(nodes, &QueryNode{
			SQL: &SQLQuery{Where: where},
		})
	}

	var query QueryNode
	switch len(nodes) {
	case 0:
		query = QueryNode{SQL: &SQLQuery{Where: map[string]interface{}{}}}
	case 1:
		query = *nodes[0]
	default:
		query = QueryNode{And: nodes}
	}

	result := ResultSpec{
		Limit:  f.Limit,
		Offset: f.Offset,
	}
	if len(f.OrderBy) > 0 {
		result.OrderBy = f.OrderBy
	} else if hasSearch {
		result.OrderBy = []OrderBySpec{{Field: "_score", Direction: "desc"}}
	}

	return &QueryRequest{
		Collection: f.Collection,
		Query:      query,
		Result:     result,
	}, nil
}

func mapSQLOperator(op string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(op)) {
	case "=":
		return "$eq", nil
	case "!=":
		return "$ne", nil
	case ">":
		return "$gt", nil
	case ">=":
		return "$gte", nil
	case "<":
		return "$lt", nil
	case "<=":
		return "$lte", nil
	case "LIKE":
		return "$like", nil
	default:
		return "", fmt.Errorf("unsupported sql operator: %s", op)
	}
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

func handleCollectionList(w http.ResponseWriter, r *http.Request) {
	logger.Info("Collection list request received", "method", r.Method, "remote_addr", r.RemoteAddr)

	if r.Method != http.MethodGet {
		logger.Warn("Invalid method for collection list", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only GET method is allowed")
		return
	}

	// 先檢查 collections 表是否存在
	checkTableQuery := "SELECT name FROM sqlite_master WHERE type='table' AND name='collections'"
	var tableName string
	err := db.QueryRow(checkTableQuery).Scan(&tableName)
	if err != nil {
		if err == sql.ErrNoRows {
			logger.Info("Collections table does not exist yet, returning empty list")
			respondWithJSON(w, http.StatusOK, map[string]interface{}{
				"collections": []map[string]interface{}{},
				"count":       0,
			})
			return
		}
		logger.Error("Failed to check collections table existence", "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to check database structure")
		return
	}

	// 查詢所有 collections
	query := "SELECT name, schema_json FROM collections"
	logger.Debug("Executing query", "query", query)
	rows, err := db.Query(query)
	if err != nil {
		logger.Error("Failed to query collections", "error", err, "query", query)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to query collections: %v", err))
		return
	}
	defer rows.Close()

	var collections []map[string]interface{}
	for rows.Next() {
		var name, schemaStr string
		if err := rows.Scan(&name, &schemaStr); err != nil {
			logger.Error("Failed to scan collection row", "error", err)
			continue
		}

		logger.Debug("Processing collection", "name", name, "schema_length", len(schemaStr))

		// 解析 schema 以獲取額外信息
		var schema CollectionSchema
		collection := map[string]interface{}{
			"name": name,
		}

		if err := json.Unmarshal([]byte(schemaStr), &schema); err == nil {
			collection["primary_key"] = schema.PrimaryKey
			collection["field_count"] = len(schema.Fields)
			collection["has_fts"] = schema.FTS.Stemming

			// 統計文檔數量（安全檢查 collection 名稱）
			if isValidIdentifier(name) {
				countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", name)
				var docCount int
				if err := db.QueryRow(countQuery).Scan(&docCount); err == nil {
					collection["document_count"] = docCount
					logger.Debug("Collection document count", "name", name, "count", docCount)
				} else {
					logger.Debug("Failed to count documents in collection", "name", name, "error", err)
					collection["document_count"] = 0
				}
			} else {
				logger.Warn("Invalid collection name for counting", "name", name)
				collection["document_count"] = 0
			}
		} else {
			logger.Error("Failed to parse collection schema", "name", name, "error", err)
			collection["primary_key"] = "unknown"
			collection["field_count"] = 0
			collection["has_fts"] = false
			collection["document_count"] = 0
		}

		collections = append(collections, collection)
	}

	// 檢查 rows 是否有錯誤
	if err := rows.Err(); err != nil {
		logger.Error("Error iterating over collection rows", "error", err)
		respondWithError(w, http.StatusInternalServerError, fmt.Sprintf("Error reading collections: %v", err))
		return
	}

	logger.Info("Collections listed successfully", "count", len(collections), "remote_addr", r.RemoteAddr)
	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"collections": collections,
		"count":       len(collections),
	})
}

func handleCollectionContent(w http.ResponseWriter, r *http.Request) {
	logger.Info("Collection content request received", "method", r.Method, "remote_addr", r.RemoteAddr)

	if r.Method != http.MethodGet {
		logger.Warn("Invalid method for collection content", "method", r.Method, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusMethodNotAllowed, "Only GET method is allowed")
		return
	}

	collectionName := r.URL.Query().Get("collection")
	if collectionName == "" {
		respondWithError(w, http.StatusBadRequest, "Collection parameter is required")
		return
	}

	if !isValidIdentifier(collectionName) {
		logger.Warn("Invalid collection name for content", "collection_name", collectionName, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, "Invalid collection name")
		return
	}

	// 解析分頁參數
	page := 1
	limit := 20
	if pageParam := r.URL.Query().Get("page"); pageParam != "" {
		if p, err := parsePositiveInt(pageParam); err == nil {
			page = p
		}
	}
	if limitParam := r.URL.Query().Get("limit"); limitParam != "" {
		if l, err := parsePositiveInt(limitParam); err == nil && l <= 100 {
			limit = l
		}
	}

	offset := (page - 1) * limit

	// 檢查 collection 是否存在
	schema, err := getCollectionSchema(collectionName)
	if err != nil {
		logger.Error("Collection not found", "collection", collectionName, "error", err)
		respondWithError(w, http.StatusNotFound, fmt.Sprintf("Collection '%s' not found", collectionName))
		return
	}

	// 獲取總記錄數
	var totalCount int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", collectionName)
	if err := db.QueryRow(countQuery).Scan(&totalCount); err != nil {
		logger.Error("Failed to count records", "collection", collectionName, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to count records")
		return
	}

	// 獲取記錄資料
	query := fmt.Sprintf("SELECT * FROM %s ORDER BY rowid LIMIT ? OFFSET ?", collectionName)
	rows, err := db.Query(query, limit, offset)
	if err != nil {
		logger.Error("Failed to query collection content", "collection", collectionName, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to query collection content")
		return
	}
	defer rows.Close()

	// 獲取欄位名稱
	columns, err := rows.Columns()
	if err != nil {
		logger.Error("Failed to get columns", "collection", collectionName, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Failed to get columns")
		return
	}

	// 讀取資料
	var records []map[string]interface{}
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePointers := make([]interface{}, len(columns))
		for i := range values {
			valuePointers[i] = &values[i]
		}

		if err := rows.Scan(valuePointers...); err != nil {
			logger.Error("Failed to scan row", "collection", collectionName, "error", err)
			continue
		}

		record := make(map[string]interface{})
		for i, column := range columns {
			record[column] = values[i]
		}
		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		logger.Error("Error iterating over rows", "collection", collectionName, "error", err)
		respondWithError(w, http.StatusInternalServerError, "Error reading collection content")
		return
	}

	totalPages := (totalCount + limit - 1) / limit

	logger.Info("Collection content retrieved successfully",
		"collection", collectionName,
		"total_count", totalCount,
		"page", page,
		"limit", limit,
		"records_count", len(records),
		"remote_addr", r.RemoteAddr)

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"collection": collectionName,
		"schema":     schema,
		"columns":    columns,
		"records":    records,
		"pagination": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total_count": totalCount,
			"total_pages": totalPages,
			"has_next":    page < totalPages,
			"has_prev":    page > 1,
		},
	})
}

func parsePositiveInt(s string) (int, error) {
	var result int
	if _, err := fmt.Sscanf(s, "%d", &result); err != nil {
		return 0, err
	}
	if result <= 0 {
		return 0, fmt.Errorf("value must be positive")
	}
	return result, nil
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
	var flatReq FlatQueryRequest
	if err := json.Unmarshal(body, &flatReq); err == nil && flatReq.hasNewFormatFields() {
		converted, err := flatReq.toQueryRequest()
		if err != nil {
			logger.Warn("Invalid flat query request", "error", err, "remote_addr", r.RemoteAddr)
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		req = *converted
	} else {
		if err := json.Unmarshal(body, &req); err != nil {
			logger.Error("Failed to unmarshal JSON for query", "error", err)
			respondWithError(w, http.StatusBadRequest, "Invalid JSON format")
			return
		}
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
