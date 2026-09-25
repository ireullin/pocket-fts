package pocketfts

import (
	"context"
	"database/sql"
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

// writeDocument runs one document write as a single unit. Under the
// store-wide write lock, sqlStep runs inside a transaction, then ftsStep (if
// any) writes the full-text index, and only then does the transaction commit.
// A failing ftsStep rolls the SQL half back, so the table and the index change
// together or not at all. The one gap left is ftsStep succeeding and the
// commit then failing.
//
// The write timeout covers waiting for the lock and the SQL statement.
func (s *Store) writeDocument(ctx context.Context, sqlStep func(context.Context, *sql.Tx) error, ftsStep func() error) error {
	ctx, cancel := context.WithTimeout(ctx, s.writeTimeout)
	defer cancel()

	release, err := s.acquireWrite(ctx)
	if err != nil {
		return err
	}
	defer release()

	// The transaction does not inherit the deadline: database/sql rolls a
	// transaction back when its context ends, and once ftscore has taken the
	// write, a deadline must not undo the SQL half.
	tx, err := s.writeDB.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return fmt.Errorf("failed to begin write transaction: %w", err)
	}
	if err := sqlStep(ctx, tx); err != nil {
		tx.Rollback()
		return err
	}
	if s.betweenWrites != nil {
		s.betweenWrites()
	}
	if ftsStep != nil {
		if err := ftsStep(); err != nil {
			tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		s.log.Error("Inconsistency: full-text index written but SQL commit failed", "error", err)
		return fmt.Errorf("failed to commit write: %w", err)
	}
	return nil
}

// sqlWriteError turns an error from a statement inside writeDocument into the
// error the caller sees: ErrWriteTimeout when the deadline ran out, else msg.
func (s *Store) sqlWriteError(err error, msg string, attrs ...any) error {
	if errors.Is(err, context.DeadlineExceeded) {
		s.log.Warn("SQL write timed out", append(attrs, "timeout", s.writeTimeout)...)
		return fmt.Errorf("%w after %s", ErrWriteTimeout, s.writeTimeout)
	}
	s.log.Error(msg, append(attrs, "error", err)...)
	return errors.New(msg)
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

	upsertSQL, values, err := generateUpsertSQL(collection, doc)
	if err != nil {
		s.log.Error("Failed to generate upsert SQL", "collection", collection, "error", err)
		return errors.New("Could not process document for SQL upsert")
	}

	sqlStep := func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, upsertSQL, values...); err != nil {
			return s.sqlWriteError(err, "Failed to save document to SQL table", "collection", collection)
		}
		return nil
	}

	// The full-text index is written only if this collection has an indexed field.
	var ftsStep func() error
	if schemaHasFTS(schema) {
		ftsStep = func() error {
			if err := s.fts.UpsertDocument(collection, string(docBytes)); err != nil {
				if isTimeoutError(err) {
					s.log.Warn("Upsert to FTS timed out", "collection", collection, "timeout", s.writeTimeout)
					return fmt.Errorf("%w: %v", ErrWriteTimeout, err)
				}
				s.log.Error("Failed to upsert document to FTS", "collection", collection, "error", err)
				return fmt.Errorf("Failed to upsert document: %w", err)
			}
			return nil
		}
	}

	return s.writeDocument(ctx, sqlStep, ftsStep)
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

	deleteSQL := fmt.Sprintf("DELETE FROM %s WHERE %s = ?", collection, schema.PrimaryKey) // Safe due to checks
	sqlStep := func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, deleteSQL, id); err != nil {
			return s.sqlWriteError(err, "Failed to delete document from SQL table", "collection", collection, "id", id)
		}
		return nil
	}

	// The full-text index is written only if this collection has an indexed field.
	var ftsStep func() error
	if schemaHasFTS(schema) {
		ftsStep = func() error {
			primaryKeyJSON := fmt.Sprintf("{\"%s\":\"%s\"}", schema.PrimaryKey, id)
			if err := s.fts.DeleteDocument(collection, primaryKeyJSON); err != nil {
				if isTimeoutError(err) {
					s.log.Warn("Delete from FTS timed out", "collection", collection, "id", id, "timeout", s.writeTimeout)
					return fmt.Errorf("%w: %v", ErrWriteTimeout, err)
				}
				s.log.Error("Failed to delete document from FTS", "collection", collection, "id", id, "error", err)
				return fmt.Errorf("Failed to delete document from FTS: %w", err)
			}
			return nil
		}
	}

	return s.writeDocument(ctx, sqlStep, ftsStep)
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
