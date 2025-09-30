package main

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite" // Register sqlite driver
)

// initDB initializes the SQLite database and creates the necessary tables.
func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Create collections table to store schema information
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS collections (
		name TEXT NOT NULL PRIMARY KEY,
		schema_json TEXT NOT NULL
	);`

	_, err = db.Exec(createTableSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to create collections table: %w", err)
	}

	return db, nil
}

// saveCollectionSchema saves or updates a collection's schema in the database.
func saveCollectionSchema(name, schemaJSON string) error {
	sqlStmt := `
	INSERT INTO collections (name, schema_json) VALUES (?, ?)
	ON CONFLICT(name) DO UPDATE SET schema_json = excluded.schema_json;
	`
	_, err := db.Exec(sqlStmt, name, schemaJSON)
	if err != nil {
		return fmt.Errorf("failed to save schema for collection '%s': %w", name, err)
	}
	return nil
}

// getCollectionSchema retrieves a collection's schema from the database.
func getCollectionSchema(name string) (string, error) {
	var schemaJSON string
	sqlStmt := `SELECT schema_json FROM collections WHERE name = ?;`
	err := db.QueryRow(sqlStmt, name).Scan(&schemaJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("collection '%s' not found", name)
		}
		return "", fmt.Errorf("failed to get schema for collection '%s': %w", name, err)
	}
	return schemaJSON, nil
}

// deleteCollectionSchema deletes a collection's schema from the database.
func deleteCollectionSchema(name string) error {
	sqlStmt := `DELETE FROM collections WHERE name = ?;`
	res, err := db.Exec(sqlStmt, name)
	if err != nil {
		return fmt.Errorf("failed to delete schema for collection '%s': %w", name, err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to check rows affected after deleting collection '%s': %w", name, err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("collection '%s' not found, nothing deleted", name)
	}
	return nil
}