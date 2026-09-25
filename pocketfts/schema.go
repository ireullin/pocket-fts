package pocketfts

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Field struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Weight     float64 `json:"weight,omitempty"`
	Searchable bool    `json:"searchable,omitempty"`
	PrimaryKey bool    `json:"primary_key,omitempty"` // Added for convenience
}

type FTSConfig struct {
	Stemming bool `json:"stemming"`
}

type CollectionSchema struct {
	Name       string     `json:"name"`
	PrimaryKey string     `json:"primary_key"`
	FTS        FTSConfig  `json:"fts"`
	Fields     []Field    `json:"fields"`
	Indexes    [][]string `json:"indexes,omitempty"`
}

// Schema is the collection schema accepted by Store.CreateCollection.
type Schema = CollectionSchema

// Document is one record: field name to value.
type Document = map[string]interface{}

// 識別字必須整串都是英數與底線。這個 regex 一定要頭尾都錨定：只錨定開頭的話
// `id; DROP TABLE x--` 這種字串也會通過檢查，而 collection 名稱與欄位名稱
// 是直接用 Sprintf 串進 SQL 的。
var validIdentifierRegex = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	return validIdentifierRegex.MatchString(name)
}

// schemaHasFTS 判斷這個 collection 是否有任何欄位要被全文搜尋。ftscore 只
// 提供全文索引功能，一個 collection 一個 searchable 欄位都沒有時，不該碰
// ftscore——建立、寫入、刪除都只作用在 db.sqlite。
func schemaHasFTS(schema CollectionSchema) bool {
	for _, field := range schema.Fields {
		if field.Searchable {
			return true
		}
	}
	return false
}

// ftsField and ftsSchema mirror Field/CollectionSchema but with the JSON key
// ftscore's own schema parser expects ("indexed") instead of pocket-fts's
// public API key ("searchable"). They exist only to build the payload sent
// to fts.CreateCollection.
//
// The per-field primary_key flag is left out on purpose: ftscore rejects it
// and takes the primary key from the schema level only.
type ftsField struct {
	Name    string  `json:"name"`
	Type    string  `json:"type"`
	Weight  float64 `json:"weight,omitempty"`
	Indexed bool    `json:"indexed,omitempty"`
}

type ftsSchema struct {
	Name       string     `json:"name"`
	PrimaryKey string     `json:"primary_key"`
	FTS        FTSConfig  `json:"fts"`
	Fields     []ftsField `json:"fields"`
}

// schemaForFTS builds the JSON payload to send to fts.CreateCollection.
// pocket-fts's public schema key is "searchable"; ftscore's own parser
// expects "indexed". The raw request body can no longer be forwarded
// verbatim — this translates the key so ftscore keeps working unchanged.
func schemaForFTS(schema CollectionSchema) ([]byte, error) {
	out := ftsSchema{Name: schema.Name, PrimaryKey: schema.PrimaryKey, FTS: schema.FTS}
	for _, field := range schema.Fields {
		out.Fields = append(out.Fields, ftsField{
			Name:    field.Name,
			Type:    field.Type,
			Weight:  field.Weight,
			Indexed: field.Searchable,
		})
	}
	return json.Marshal(out)
}

// validateFieldPrimaryKeyFlags checks the per-field primary_key convenience
// flag: a field may carry it only if it is the schema's primary key.
func validateFieldPrimaryKeyFlags(schema CollectionSchema) error {
	for _, field := range schema.Fields {
		if field.PrimaryKey && !strings.EqualFold(field.Name, schema.PrimaryKey) {
			return fmt.Errorf("field %q is flagged primary_key but the collection's primary_key is %q",
				field.Name, schema.PrimaryKey)
		}
	}
	return nil
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

// generateIndexSQL turns schema.Indexes into one CREATE INDEX statement per
// entry — one field is a single-column index, several fields is a composite
// index over exactly those columns in the declared order. Column order
// matters for SQLite's query planner on a composite index, so the order in
// the schema is preserved as-is; it is the caller's responsibility to
// declare it correctly.
//
// This is independent of searchable (the full-text flag): a field ends up
// in a SQL index purely because it's listed here, regardless of whether
// it's also indexed by ftscore.
func generateIndexSQL(schema CollectionSchema) ([]string, error) {
	known := make(map[string]bool, len(schema.Fields))
	for _, field := range schema.Fields {
		known[field.Name] = true
	}

	if !isValidIdentifier(schema.Name) {
		return nil, fmt.Errorf("invalid collection name: %s", schema.Name)
	}

	var statements []string
	for _, fields := range schema.Indexes {
		if len(fields) == 0 {
			return nil, fmt.Errorf("index entry must contain at least one field")
		}
		for _, name := range fields {
			if !known[name] {
				return nil, fmt.Errorf("index references unknown field: %s", name)
			}
			// isValidIdentifier is checked here, not inferred from
			// generateCreateTableSQL having run first — this function must
			// be safe to call standalone, independent of call order.
			if !isValidIdentifier(name) {
				return nil, fmt.Errorf("invalid field name: %s", name)
			}
		}
		indexName := fmt.Sprintf("idx_%s_%s", schema.Name, strings.Join(fields, "_"))
		// Safe to use Sprintf: schema.Name and every field name in `fields`
		// were just validated above, in this function.
		statements = append(statements, fmt.Sprintf(
			"CREATE INDEX IF NOT EXISTS %s ON %s (%s)", indexName, schema.Name, strings.Join(fields, ", ")))
	}
	return statements, nil
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
