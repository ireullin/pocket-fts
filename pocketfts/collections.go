package pocketfts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// CreateCollection creates a collection: its SQL table, its secondary indexes,
// and, when any field is searchable, its full-text index.
func (s *Store) CreateCollection(ctx context.Context, schema Schema) error {
	raw, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("failed to encode schema: %w", err)
	}
	return s.createCollection(ctx, schema, string(raw))
}

// CreateCollectionJSON creates a collection from its JSON schema, and stores
// that JSON exactly as given.
func (s *Store) CreateCollectionJSON(ctx context.Context, data []byte) error {
	var schema CollectionSchema
	if err := json.Unmarshal(data, &schema); err != nil {
		s.log.Error("Failed to unmarshal JSON for collection create", "error", err)
		return &ValidationError{Message: "Invalid JSON format"}
	}
	return s.createCollection(ctx, schema, string(data))
}

func (s *Store) createCollection(ctx context.Context, schema CollectionSchema, schemaJSON string) error {
	if !isValidIdentifier(schema.Name) {
		s.log.Warn("Invalid collection name provided", "collection_name", schema.Name)
		return &ValidationError{Message: "Invalid collection name. Must be alphanumeric."}
	}

	// Validate (and pre-build) the secondary-index statements before doing
	// anything else, so a bad "indexes" entry fails the whole request with
	// 400 and creates nothing — no FTS collection, no metadata, no table.
	createIndexSQL, err := generateIndexSQL(schema)
	if err != nil {
		s.log.Warn("Invalid indexes in collection schema", "collection", schema.Name, "error", err)
		return newValidationError("Invalid indexes: %v", err)
	}

	// 1. Create FTS collection — only if this schema actually has a field to index.
	// ftscore only provides full-text indexing; a collection with no indexed
	// fields is a plain SQL table and never touches it.
	if schemaHasFTS(schema) {
		ftsPayload, err := schemaForFTS(schema)
		if err != nil {
			s.log.Error("Failed to build FTS schema payload", "collection", schema.Name, "error", err)
			return errors.New("Failed to build FTS schema payload")
		}
		if err := s.fts.CreateCollection(string(ftsPayload)); err != nil {
			s.log.Error("Failed to create FTS collection", "collection", schema.Name, "error", err)
			return fmt.Errorf("Failed to create FTS collection: %w", err)
		}
	}

	// 2. Save schema to our metadata table
	if err := s.saveSchema(ctx, schema.Name, schemaJSON); err != nil {
		s.log.Error("Failed to save collection schema to DB", "collection", schema.Name, "error", err)
		return fmt.Errorf("Failed to save collection schema: %w", err)
	}

	// 3. Create the regular SQL table for storing original documents
	createTableSQL, err := generateCreateTableSQL(schema)
	if err != nil {
		s.log.Error("Failed to generate CREATE TABLE SQL", "collection", schema.Name, "error", err)
		return newValidationError("Invalid schema for SQL table creation: %v", err)
	}
	if _, err := s.execWrite(ctx, createTableSQL); err != nil {
		s.log.Error("Failed to create regular SQL table", "collection", schema.Name, "error", err)
		return fmt.Errorf("Failed to create table '%s': %w", schema.Name, err)
	}

	// 4. Build any secondary indexes declared in schema.Indexes. Independent
	// of FTS — a field ends up here purely because it was listed, regardless
	// of whether it's also searchable.
	for _, stmt := range createIndexSQL {
		if _, err := s.execWrite(ctx, stmt); err != nil {
			s.log.Error("Failed to create index", "collection", schema.Name, "statement", stmt, "error", err)
			return fmt.Errorf("Failed to create index: %w", err)
		}
	}

	return nil
}

// DeleteCollection drops a collection's table, schema and full-text index.
func (s *Store) DeleteCollection(ctx context.Context, name string) error {
	if !isValidIdentifier(name) {
		s.log.Warn("Invalid collection name for delete", "collection_name", name)
		return &ValidationError{Message: "Invalid collection name."}
	}

	// 1. Look up the schema before deleting the metadata row — need it to
	// know whether this collection ever had an indexed field, and it won't
	// be readable anymore once step 2 removes it. If it can't be read or
	// parsed, fail safe to the pre-existing behavior (still try the FTS
	// delete) rather than silently skip it.
	hasFTS := true
	if schemaString, err := loadSchemaJSON(ctx, s.db, name); err == nil {
		var schema CollectionSchema
		if err := json.Unmarshal([]byte(schemaString), &schema); err == nil {
			hasFTS = schemaHasFTS(schema)
		}
	}

	// 2. Delete from our metadata
	if err := s.deleteSchema(ctx, name); err != nil {
		s.log.Error("Failed to delete collection schema from DB", "collection", name, "error", err)
		return fmt.Errorf("Failed to delete collection schema: %w", err)
	}

	// 3. Delete from the FTS engine — only if it was ever created there.
	if hasFTS {
		if err := s.fts.DeleteCollection(name); err != nil {
			s.log.Error("Inconsistency: failed to delete from FTS engine", "collection", name, "error", err)
		}
	}

	// 4. Drop the regular SQL table
	dropTableSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", name) // Safe due to isValidIdentifier check
	if _, err := s.execWrite(ctx, dropTableSQL); err != nil {
		s.log.Error("Inconsistency: failed to drop regular SQL table", "collection", name, "error", err)
	}

	return nil
}

// CollectionInfo summarizes one collection. The fields are in alphabetical
// order so its JSON matches the key order of a marshaled map.
type CollectionInfo struct {
	DocumentCount int    `json:"document_count"`
	FieldCount    int    `json:"field_count"`
	HasFTS        bool   `json:"has_fts"`
	Name          string `json:"name"`
	PrimaryKey    string `json:"primary_key"`
}

// ListCollections lists every collection. It returns nil when there are none.
func (s *Store) ListCollections(ctx context.Context) ([]CollectionInfo, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name, schema_json FROM collections")
	if err != nil {
		s.log.Error("Failed to query collections", "error", err)
		return nil, fmt.Errorf("Failed to query collections: %w", err)
	}
	defer rows.Close()

	var collections []CollectionInfo
	for rows.Next() {
		var name, schemaStr string
		if err := rows.Scan(&name, &schemaStr); err != nil {
			s.log.Error("Failed to scan collection row", "error", err)
			continue
		}

		// 解析 schema 以獲取額外信息
		info := CollectionInfo{Name: name}
		var schema CollectionSchema
		if err := json.Unmarshal([]byte(schemaStr), &schema); err == nil {
			info.PrimaryKey = schema.PrimaryKey
			info.FieldCount = len(schema.Fields)
			info.HasFTS = schemaHasFTS(schema)

			// 統計文檔數量（安全檢查 collection 名稱）
			if isValidIdentifier(name) {
				countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", name)
				if err := s.db.QueryRowContext(ctx, countQuery).Scan(&info.DocumentCount); err != nil {
					s.log.Debug("Failed to count documents in collection", "name", name, "error", err)
					info.DocumentCount = 0
				}
			} else {
				s.log.Warn("Invalid collection name for counting", "name", name)
			}
		} else {
			s.log.Error("Failed to parse collection schema", "name", name, "error", err)
			info.PrimaryKey = "unknown"
		}

		collections = append(collections, info)
	}

	if err := rows.Err(); err != nil {
		s.log.Error("Error iterating over collection rows", "error", err)
		return nil, fmt.Errorf("Error reading collections: %w", err)
	}
	return collections, nil
}

// Content is one page of a collection's rows, in insertion order.
type Content struct {
	// Schema is the collection's schema JSON as it was stored.
	Schema     string
	Columns    []string
	Records    []map[string]interface{}
	TotalCount int
}

// CollectionContent returns one page of a collection's rows. page starts at 1.
func (s *Store) CollectionContent(ctx context.Context, name string, page, limit int) (*Content, error) {
	if !isValidIdentifier(name) {
		s.log.Warn("Invalid collection name for content", "collection_name", name)
		return nil, &ValidationError{Message: "Invalid collection name"}
	}
	offset := (page - 1) * limit

	// 檢查 collection 是否存在
	schema, err := loadSchemaJSON(ctx, s.db, name)
	if err != nil {
		s.log.Error("Collection not found", "collection", name, "error", err)
		return nil, &NotFoundError{Message: fmt.Sprintf("Collection '%s' not found", name)}
	}

	// 獲取總記錄數
	content := &Content{Schema: schema}
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM %s", name)
	if err := s.db.QueryRowContext(ctx, countQuery).Scan(&content.TotalCount); err != nil {
		s.log.Error("Failed to count records", "collection", name, "error", err)
		return nil, errors.New("Failed to count records")
	}

	// 獲取記錄資料
	query := fmt.Sprintf("SELECT * FROM %s ORDER BY rowid LIMIT ? OFFSET ?", name)
	rows, err := s.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		s.log.Error("Failed to query collection content", "collection", name, "error", err)
		return nil, errors.New("Failed to query collection content")
	}
	defer rows.Close()

	// 獲取欄位名稱
	columns, err := rows.Columns()
	if err != nil {
		s.log.Error("Failed to get columns", "collection", name, "error", err)
		return nil, errors.New("Failed to get columns")
	}
	content.Columns = columns

	// 讀取資料
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePointers := make([]interface{}, len(columns))
		for i := range values {
			valuePointers[i] = &values[i]
		}

		if err := rows.Scan(valuePointers...); err != nil {
			s.log.Error("Failed to scan row", "collection", name, "error", err)
			continue
		}

		record := make(map[string]interface{})
		for i, column := range columns {
			record[column] = values[i]
		}
		content.Records = append(content.Records, record)
	}

	if err := rows.Err(); err != nil {
		s.log.Error("Error iterating over rows", "collection", name, "error", err)
		return nil, errors.New("Error reading collection content")
	}
	return content, nil
}
