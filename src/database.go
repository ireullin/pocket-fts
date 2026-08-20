package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // Register sqlite driver
)

// DefaultWriteTimeout 是寫入等待寫入連線加上執行的總時限。
const DefaultWriteTimeout = 30 * time.Second

// writeTimeout 由 main 依 -write-timeout 參數設定。
var writeTimeout = DefaultWriteTimeout

// SetWriteTimeout 設定寫入的時限。非正值會被忽略，維持預設值。
func SetWriteTimeout(d time.Duration) {
	if d > 0 {
		writeTimeout = d
	}
}

// ErrWriteTimeout 表示寫入在排隊等待寫入連線的期間超過了時限。
// 這代表服務當下的寫入量超過它的吞吐能力，不是請求本身有問題。
var ErrWriteTimeout = errors.New("write timed out")

// execWrite 透過寫入連線池執行一個寫入語句。
//
// 寫入池只有一條連線，所以同時進來的寫入會在 database/sql 的連線池裡排隊，
// 而不是在 SQLite 那層搶鎖。搶鎖會受 busy_timeout 管轄，等太久就直接失敗；
// 排隊則是等待，錯誤變成延遲。但排隊不能無止境，所以整段（等連線加上執行）
// 共用一個 writeTimeout 的期限，超過就回 ErrWriteTimeout。
func execWrite(query string, args ...interface{}) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	result, err := writeDB.ExecContext(ctx, query, args...)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w after %s", ErrWriteTimeout, writeTimeout)
	}
	return result, err
}

// dsn 組出連線字串。busy_timeout 用 DSN 傳（而不是單次 PRAGMA Exec），
// 讓連線池開的每一條連線都套用得到。
func dsn(dbPath string) string {
	return fmt.Sprintf("%s?_pragma=busy_timeout(5000)", dbPath)
}

// initWriteDB 開啟專供寫入使用的連線池。
//
// SQLite 同一時間只允許一個寫入者。把池子限制成一條連線，寫入就在 Go 這側
// 依序排隊；若讓多條連線去搶 SQLite 的鎖，搶輸的會在 busy_timeout 到期後
// 回傳 SQLITE_BUSY，呼叫端收到 500，寫入等於被丟掉。
func initWriteDB(dbPath string) (*sql.DB, error) {
	writer, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open write database: %w", err)
	}

	writer.SetMaxOpenConns(1)
	writer.SetMaxIdleConns(1)
	writer.SetConnMaxLifetime(0)

	if err := writer.Ping(); err != nil {
		writer.Close()
		return nil, fmt.Errorf("failed to connect to write database: %w", err)
	}

	return writer, nil
}

// initDB initializes the SQLite database and creates the necessary tables.
//
// 這個池子只給讀取使用，不限制連線數。讀取可以真正併發，是唯一會隨核心數
// 成長的路徑；若把它一併限制成一條連線，範圍查詢的吞吐量會掉到三分之一。
func initDB(dbPath string) (*sql.DB, error) {
	// busy_timeout is passed via the DSN (rather than a one-off PRAGMA Exec)
	// so that every connection the pool opens - not just the one that
	// happens to run the first Exec - waits and retries on SQLITE_BUSY
	// instead of failing immediately. This matters because the same
	// db.sqlite file is also written to by the ftscore C engine on its own
	// connection, so lock contention between the two is expected.
	db, err := sql.Open("sqlite", dsn(dbPath))
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
	_, err := execWrite(sqlStmt, name, schemaJSON)
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
	res, err := execWrite(sqlStmt, name)
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
