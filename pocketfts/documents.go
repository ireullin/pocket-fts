package pocketfts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// lookupSchema reads and parses a collection's schema for a document call.
func (s *Store) lookupSchema(ctx context.Context, collection string) (CollectionSchema, error) {
	var schema CollectionSchema
	schemaString, err := loadSchemaJSON(ctx, s.db, collection)
	if err != nil {
		return schema, &NotFoundError{Message: fmt.Sprintf("Collection '%s' not found", collection)}
	}
	if err := json.Unmarshal([]byte(schemaString), &schema); err != nil {
		return schema, errors.New("Failed to parse stored collection schema")
	}
	return schema, nil
}

// Upsert inserts doc, or replaces the document with the same primary key.
func (s *Store) Upsert(ctx context.Context, collection string, doc Document) error {
	if !isValidIdentifier(collection) {
		s.log.Warn("Invalid collection name for document upsert", "collection_name", collection)
		return &ValidationError{Message: "Invalid collection name."}
	}

	docBytes, err := json.Marshal(doc)
	if err != nil {
		return &ValidationError{Message: "Failed to marshal document to JSON"}
	}

	schema, err := s.lookupSchema(ctx, collection)
	if err != nil {
		return err
	}

	// 1. Upsert to FTS index — only if this collection has an indexed field.
	if schemaHasFTS(schema) {
		if err := s.fts.UpsertDocument(collection, string(docBytes)); err != nil {
			if isTimeoutError(err) {
				s.log.Warn("Upsert to FTS timed out", "collection", collection, "timeout", s.writeTimeout)
				return fmt.Errorf("%w: %v", ErrWriteTimeout, err)
			}
			s.log.Error("Failed to upsert document to FTS", "collection", collection, "error", err)
			return fmt.Errorf("Failed to upsert document: %w", err)
		}
	}

	// 2. Upsert to regular SQL table
	upsertSQL, values, err := generateUpsertSQL(collection, doc)
	if err != nil {
		s.log.Error("Failed to generate upsert SQL", "collection", collection, "error", err)
		return errors.New("Could not process document for SQL upsert")
	}

	if _, err := s.execWrite(ctx, upsertSQL, values...); err != nil {
		if isTimeoutError(err) {
			s.log.Warn("Upsert timed out waiting for the write connection",
				"collection", collection, "timeout", s.writeTimeout)
			return err
		}
		s.log.Error("Failed to upsert document to SQL table", "collection", collection, "error", err)
		return errors.New("Failed to save document to SQL table")
	}
	return nil
}

// Delete removes the document whose primary key is id. Deleting a document
// that does not exist is not an error.
func (s *Store) Delete(ctx context.Context, collection, id string) error {
	if !isValidIdentifier(collection) {
		s.log.Warn("Invalid collection name for document delete", "collection_name", collection)
		return &ValidationError{Message: "Invalid collection name."}
	}

	schema, err := s.lookupSchema(ctx, collection)
	if err != nil {
		return err
	}

	// 1. Delete from FTS index — only if this collection has an indexed field.
	if schemaHasFTS(schema) {
		primaryKeyJSON := fmt.Sprintf("{\"%s\":\"%s\"}", schema.PrimaryKey, id)
		if err := s.fts.DeleteDocument(collection, primaryKeyJSON); err != nil {
			if isTimeoutError(err) {
				s.log.Warn("Delete from FTS timed out", "collection", collection, "id", id, "timeout", s.writeTimeout)
				return fmt.Errorf("%w: %v", ErrWriteTimeout, err)
			}
			s.log.Error("Failed to delete document from FTS", "collection", collection, "id", id, "error", err)
			return fmt.Errorf("Failed to delete document from FTS: %w", err)
		}
	}

	// 2. Delete from regular SQL table
	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", collection, schema.PrimaryKey) // Safe due to checks
	if _, err := s.execWrite(ctx, deleteSQL, id); err != nil {
		if isTimeoutError(err) {
			s.log.Warn("Delete timed out waiting for the write connection",
				"collection", collection, "timeout", s.writeTimeout)
			return err
		}
		s.log.Error("Failed to delete document from SQL table", "collection", collection, "id", id, "error", err)
		return errors.New("Failed to delete document from SQL table")
	}
	return nil
}

// Search runs a raw ftscore search request (the JSON body of /search) and
// returns ftscore's raw JSON result.
func (s *Store) Search(ctx context.Context, collection, requestJSON string) (string, error) {
	schema, err := s.lookupSchema(ctx, collection)
	if err != nil {
		return "", err
	}
	if !schemaHasFTS(schema) {
		return "", newValidationError(
			"Collection '%s' has no searchable fields; full-text search is not available", collection)
	}

	resultJSON, err := s.fts.Search(collection, requestJSON)
	if err != nil {
		s.log.Error("Failed to perform search", "collection", collection, "error", err)
		return "", fmt.Errorf("Search failed: %w", err)
	}
	return resultJSON, nil
}

// Query runs a query tree against a collection and returns the matching rows.
// Rows matched by a search clause carry their relevance as "_score".
func (s *Store) Query(ctx context.Context, collection string, q Node, r Result) ([]map[string]interface{}, error) {
	if !isValidIdentifier(collection) {
		s.log.Warn("Invalid collection name for query", "collection_name", collection)
		return nil, &ValidationError{Message: "Invalid collection name"}
	}
	return s.qe.ExecuteQuery(ctx, &QueryRequest{Collection: collection, Query: q, Result: r})
}
