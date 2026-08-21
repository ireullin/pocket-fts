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

// dsn 組出連線字串。這些 pragma 用 DSN 傳（而不是單次 PRAGMA Exec），讓連線池
// 開的每一條連線都套用得到，不會只套用到剛好執行那次 Exec 的連線。
//
//   - busy_timeout(5000)：鎖爭用時等待重試，而不是立即回 SQLITE_BUSY。
//   - journal_mode(WAL)：讀取不被進行中的寫入卡住，寫入也不必每次 commit 都
//     fsync。journal_mode 是資料庫檔案層級的屬性，理論上只要第一條連線設定過
//     就會持續生效，但每條連線都帶一樣沒有壞處，且更自我說明。
//   - synchronous(NORMAL)：SQLite 官方文件對 WAL 模式的建議值，commit 不
//     fsync、只在 checkpoint 時 fsync；換來的風險是作業系統／硬體斷電時最後
//     幾筆已 commit 的交易可能遺失（應用程式自己 crash 不會，只有斷電/系統
//     崩潰才會）。
func dsn(dbPath string) string {
	return fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
}

// verifyJournalModeWAL 確認資料庫真的跑在 WAL 模式。SQLite 在某些情況下
// （檔案系統不支援、目錄不可寫）會靜默回退到非 WAL 模式，PRAGMA
// journal_mode=WAL 本身不會報錯，只會回傳實際生效的模式名稱。啟動時檢查這個
// 回傳值，不是 wal 就視為啟動失敗，避免服務安靜地用非預期的耐久性模式跑下去。
func verifyJournalModeWAL(db *sql.DB) error {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return fmt.Errorf("failed to read journal_mode: %w", err)
	}
	if mode != "wal" {
		return fmt.Errorf("database did not enable WAL mode (journal_mode is %q); "+
			"check that the database directory is writable and on a filesystem that supports WAL", mode)
	}
	return nil
}

// checkpointWAL 把 WAL 檔案的內容寫回主資料庫檔案並清空 WAL 檔案
// （PRAGMA wal_checkpoint(TRUNCATE)）。平常運作靠 SQLite 內建的自動
// checkpoint；這個函式是給正常關閉（SIGTERM）時額外呼叫一次，讓下次啟動時
// WAL 檔案是乾淨的。
func checkpointWAL(db *sql.DB) error {
	var busy, log, checkpointed int
	if err := db.QueryRow("PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busy, &log, &checkpointed); err != nil {
		return fmt.Errorf("failed to checkpoint WAL: %w", err)
	}
	if busy != 0 {
		return fmt.Errorf("WAL checkpoint could not fully complete: another connection is writing")
	}
	return nil
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
	// See dsn() for what each pragma does and why it's passed via the DSN
	// instead of a one-off PRAGMA Exec. The same db.sqlite file is also
	// written to by the ftscore C engine on its own connection, so lock
	// contention between the two is expected regardless of journal mode.
	db, err := sql.Open("sqlite", dsn(dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := verifyJournalModeWAL(db); err != nil {
		db.Close()
		return nil, err
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
