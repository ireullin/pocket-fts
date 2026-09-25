package pocketfts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Condition is one [field, operator, value] test on the stored document.
// Operator is one of =, !=, >, >=, <, <=, LIKE, compared with SQLite's rules.
type Condition struct {
	Field    string
	Operator string
	Value    interface{}
}

// ConflictError means an Update's conditions did not hold, so nothing was
// written. Failed lists the conditions that were not true; Current is the
// stored document as it is now.
type ConflictError struct {
	Failed  []Condition
	Current Document
}

func (e *ConflictError) Error() string {
	parts := make([]string, len(e.Failed))
	for i, c := range e.Failed {
		parts[i] = fmt.Sprintf("%s %s %v", c.Field, c.Operator, c.Value)
	}
	return "update conditions not met: " + strings.Join(parts, ", ")
}

// IsConflict reports whether err means an Update's conditions did not hold.
func IsConflict(err error) bool {
	var target *ConflictError
	return errors.As(err, &target)
}

// conditionOperators maps mapSQLOperator's result for each operator Update
// accepts (=, !=, >, >=, <, <=, LIKE) back to its SQL form.
var conditionOperators = map[string]string{
	"$eq":   "=",
	"$ne":   "!=",
	"$gt":   ">",
	"$gte":  ">=",
	"$lt":   "<",
	"$lte":  "<=",
	"$like": "LIKE",
}

// Update changes the fields of doc on the existing document with the same
// primary key; fields doc leaves out keep their stored values. It never
// creates a document.
//
// With conditions, the write happens only if every condition holds for the
// stored document; otherwise Update returns a *ConflictError and writes
// nothing. The check and the write happen under the store's write lock, so
// no other write can slip between them.
//
// Errors: a *NotFoundError when the collection or the document does not
// exist, a *ValidationError for an unknown field or operator.
func (s *Store) Update(ctx context.Context, collection string, doc Document, where []Condition) error {
	if !isValidIdentifier(collection) {
		return &ValidationError{Message: "Invalid collection name."}
	}
	schema, err := s.lookupSchema(ctx, collection)
	if err != nil {
		return err
	}

	id, setColumns, setValues, err := updateAssignments(&schema, doc)
	if err != nil {
		return err
	}
	conditionSQL, conditionArgs, err := updateConditions(&schema, where)
	if err != nil {
		return err
	}

	pk := schema.PrimaryKey
	var merged Document
	sqlStep := func(ctx context.Context, tx *sql.Tx) error {
		current, err := selectDocument(ctx, tx, collection, pk, id)
		if err != nil {
			return s.sqlWriteError(err, "Failed to read document for update", "collection", collection)
		}
		if current == nil {
			return &NotFoundError{Message: fmt.Sprintf("Document '%v' not found in collection '%s'", id, collection)}
		}

		if failed, err := failedConditions(ctx, tx, collection, pk, id, where, conditionSQL, conditionArgs); err != nil {
			return s.sqlWriteError(err, "Failed to check update conditions", "collection", collection)
		} else if len(failed) > 0 {
			return &ConflictError{Failed: failed, Current: current}
		}

		if len(setColumns) > 0 {
			assignments := make([]string, len(setColumns))
			for i, column := range setColumns {
				assignments[i] = column + " = ?"
			}
			// Safe to use Sprintf: collection, pk and every column were
			// validated against the schema's identifiers.
			updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE %s = ?", collection, strings.Join(assignments, ", "), pk)
			if _, err := tx.ExecContext(ctx, updateSQL, append(append([]interface{}{}, setValues...), id)...); err != nil {
				return s.sqlWriteError(err, "Failed to save document to SQL table", "collection", collection)
			}
		}

		merged = current
		for key, value := range doc {
			merged[columnName(&schema, key)] = value
		}
		return nil
	}

	var ftsStep func() error
	if schemaHasFTS(schema) && len(setColumns) > 0 {
		ftsStep = func() error {
			// ftscore gets the whole merged document; a field left NULL is
			// left out, the same as an upsert that never set it.
			full := make(Document, len(merged))
			for key, value := range merged {
				if value != nil {
					full[key] = value
				}
			}
			docBytes, err := json.Marshal(full)
			if err != nil {
				return fmt.Errorf("failed to encode document for FTS: %w", err)
			}
			err = s.fts.UpsertDocument(collection, string(docBytes))
			return s.ftsWriteError(err, "Failed to update document", "collection", collection)
		}
	}

	return s.writeDocument(ctx, sqlStep, ftsStep)
}

// columnName returns the schema's spelling of a field name, matched without
// regard to case the way SQLite matches column names.
func columnName(schema *CollectionSchema, name string) string {
	if strings.EqualFold(name, schema.PrimaryKey) {
		return schema.PrimaryKey
	}
	for _, field := range schema.Fields {
		if strings.EqualFold(name, field.Name) {
			return field.Name
		}
	}
	return name
}

// updateAssignments validates doc against the schema and splits it into the
// primary key value and the columns to set, in a stable order.
func updateAssignments(schema *CollectionSchema, doc Document) (interface{}, []string, []interface{}, error) {
	known := knownFieldNames(schema)
	var id interface{}
	hasID := false
	keys := make([]string, 0, len(doc))
	for key := range doc {
		if !isValidIdentifier(key) {
			return nil, nil, nil, newValidationError("invalid field name in document: %q", key)
		}
		if _, ok := known[strings.ToLower(key)]; !ok {
			return nil, nil, nil, newValidationError("unknown document field %q in collection %q", key, schema.Name)
		}
		if strings.EqualFold(key, schema.PrimaryKey) {
			id, hasID = doc[key], true
			continue
		}
		keys = append(keys, key)
	}
	if !hasID || id == nil {
		return nil, nil, nil, newValidationError("document must include the primary key %q", schema.PrimaryKey)
	}
	sort.Strings(keys)

	values := make([]interface{}, len(keys))
	for i, key := range keys {
		values[i] = doc[key]
	}
	return id, keys, values, nil
}

// updateConditions validates the conditions and builds one SQL expression
// per condition, with its argument.
func updateConditions(schema *CollectionSchema, where []Condition) ([]string, []interface{}, error) {
	known := knownFieldNames(schema)
	clauses := make([]string, len(where))
	args := make([]interface{}, len(where))
	for i, c := range where {
		if !isValidIdentifier(c.Field) {
			return nil, nil, newValidationError("invalid where field: %q", c.Field)
		}
		if _, ok := known[strings.ToLower(c.Field)]; !ok {
			return nil, nil, newValidationError("unknown where field %q in collection %q", c.Field, schema.Name)
		}
		mapped, err := mapSQLOperator(c.Operator)
		if err != nil {
			return nil, nil, newValidationError("%v", err)
		}
		clauses[i] = fmt.Sprintf("%s %s ?", c.Field, conditionOperators[mapped])
		args[i] = c.Value
	}
	return clauses, args, nil
}

// selectDocument reads one row by primary key. It returns nil when there is
// no such row.
func selectDocument(ctx context.Context, tx *sql.Tx, collection, pk string, id interface{}) (Document, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("SELECT * FROM %s WHERE %s = ?", collection, pk), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	values := make([]interface{}, len(columns))
	pointers := make([]interface{}, len(columns))
	for i := range values {
		pointers[i] = &values[i]
	}
	if err := rows.Scan(pointers...); err != nil {
		return nil, err
	}
	doc := make(Document, len(columns))
	for i, column := range columns {
		doc[column] = values[i]
	}
	return doc, rows.Err()
}

// failedConditions evaluates each condition against the stored row with
// SQLite's own comparison rules and returns the ones that are not true.
// A condition that evaluates to NULL counts as not true.
func failedConditions(ctx context.Context, tx *sql.Tx, collection, pk string, id interface{}, where []Condition, clauses []string, args []interface{}) ([]Condition, error) {
	var failed []Condition
	for i, clause := range clauses {
		var holds sql.NullBool
		query := fmt.Sprintf("SELECT (%s) FROM %s WHERE %s = ?", clause, collection, pk)
		if err := tx.QueryRowContext(ctx, query, args[i], id).Scan(&holds); err != nil {
			return nil, err
		}
		if !holds.Valid || !holds.Bool {
			failed = append(failed, where[i])
		}
	}
	return failed, nil
}
