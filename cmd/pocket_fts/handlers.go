package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/ireullin/pocket-fts/pocketfts"
)

// --- Structs for API Payloads ---

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

// writeContext is the context handed to the store for writes. A write is not
// tied to the request: a client that disconnects does not abort it halfway.
// The store bounds each write with its own write timeout.
func writeContext() context.Context {
	return context.Background()
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

	if err := store.CreateCollectionJSON(writeContext(), body); err != nil {
		if pocketfts.IsValidation(err) {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var created struct {
		Name string `json:"name"`
	}
	json.Unmarshal(body, &created)
	logger.Info("Successfully created collection, FTS index, and SQL table", "collection", created.Name)
	respondWithJSON(w, http.StatusCreated, map[string]string{"status": "success", "collection": created.Name})
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

	if err := store.DeleteCollection(writeContext(), req.Name); err != nil {
		if pocketfts.IsValidation(err) {
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondWithError(w, http.StatusInternalServerError, err.Error())
		return
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

	collections, err := store.ListCollections(r.Context())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, err.Error())
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

	content, err := store.CollectionContent(r.Context(), collectionName, page, limit)
	if err != nil {
		switch {
		case pocketfts.IsValidation(err):
			respondWithError(w, http.StatusBadRequest, err.Error())
		case pocketfts.IsNotFound(err):
			respondWithError(w, http.StatusNotFound, err.Error())
		default:
			respondWithError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	totalPages := (content.TotalCount + limit - 1) / limit

	logger.Info("Collection content retrieved successfully",
		"collection", collectionName,
		"total_count", content.TotalCount,
		"page", page,
		"limit", limit,
		"records_count", len(content.Records),
		"remote_addr", r.RemoteAddr)

	respondWithJSON(w, http.StatusOK, map[string]interface{}{
		"collection": collectionName,
		"schema":     content.Schema,
		"columns":    content.Columns,
		"records":    content.Records,
		"pagination": map[string]interface{}{
			"page":        page,
			"limit":       limit,
			"total_count": content.TotalCount,
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

// respondWithWriteError maps a store error from a document write to its
// HTTP status.
func respondWithWriteError(w http.ResponseWriter, err error) {
	switch {
	case pocketfts.IsValidation(err):
		respondWithError(w, http.StatusBadRequest, err.Error())
	case pocketfts.IsNotFound(err):
		respondWithError(w, http.StatusNotFound, err.Error())
	case pocketfts.IsTimeout(err):
		respondWithBusy(w, "Write timed out; the server is saturated with writes")
	default:
		respondWithError(w, http.StatusInternalServerError, err.Error())
	}
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

	if err := store.Upsert(writeContext(), req.Collection, req.Document); err != nil {
		respondWithWriteError(w, err)
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

	if err := store.Delete(writeContext(), req.Collection, req.ID); err != nil {
		respondWithWriteError(w, err)
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

	resultJSON, err := store.Search(r.Context(), req.Collection, string(body))
	if err != nil {
		switch {
		case pocketfts.IsValidation(err):
			respondWithError(w, http.StatusBadRequest, err.Error())
		case pocketfts.IsNotFound(err):
			respondWithError(w, http.StatusNotFound, err.Error())
		default:
			respondWithError(w, http.StatusInternalServerError, err.Error())
		}
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

	req, err := pocketfts.ParseQuery(body)
	if err != nil {
		logger.Warn("Invalid query request", "error", err, "remote_addr", r.RemoteAddr)
		respondWithError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 執行查詢
	records, err := store.Query(r.Context(), req.Collection, req.Query, req.Result)
	if err != nil {
		if pocketfts.IsValidation(err) {
			logger.Warn("Invalid query request", "collection", req.Collection, "error", err, "remote_addr", r.RemoteAddr)
			respondWithError(w, http.StatusBadRequest, err.Error())
			return
		}
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

// respondWithBusy 回報服務當下的寫入量超過吞吐能力。用 503 而不是 500，
// 是要讓呼叫端分得出「稍後重試會成功」與「這個請求本身有問題」。
func respondWithBusy(w http.ResponseWriter, message string) {
	w.Header().Set("Retry-After", "1")
	respondWithError(w, http.StatusServiceUnavailable, message)
}

func respondWithError(w http.ResponseWriter, code int, message string) {
	respondWithJSON(w, code, map[string]string{"error": message})
}

func respondWithJSON(w http.ResponseWriter, code int, payload interface{}) {
	response, _ := json.Marshal(payload)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
