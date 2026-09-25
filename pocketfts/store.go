// Package pocketfts is the pocket-fts engine as an importable library: SQLite
// tables for the documents, plus the embedded ftscore library for full-text
// search on the fields declared searchable.
//
// The HTTP server in cmd/pocket_fts is a thin layer over this package.
//
// Process-wide limits, all inherited from the ftscore library, which is loaded
// once per process through dlopen:
//
//   - The ftscore call timeout and log callback are shared by the whole
//     process. The most recently opened Store sets both.
//   - Open one Store per process. A second Store works, but shares those
//     settings with the first.
//   - In the default mode ftscore holds its .indices file with an exclusive
//     lock, so only one process at a time can open a given database.
package pocketfts

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Config holds the settings for Open.
type Config struct {
	// Path is the SQLite database file. The full-text index is kept next to
	// it as <name>.indices, and the ftscore library is extracted as
	// <name>.indices2.
	Path string
	// WriteTimeout bounds a write, waiting included. It also becomes the
	// ftscore call timeout, which applies to searches too. Zero means
	// DefaultWriteTimeout.
	WriteTimeout time.Duration
	// Logger receives the store's logs and the ftscore library's logs.
	// Nil discards them.
	Logger *slog.Logger
	// FTSWAL opens the full-text index in WAL mode instead of the default
	// exclusive rollback-journal mode.
	FTSWAL bool
}

// Store is one open pocket-fts database.
type Store struct {
	db           *sql.DB
	writeDB      *sql.DB
	fts          *ftsEngine
	qe           *QueryExecutor
	log          *slog.Logger
	writeTimeout time.Duration
}

// ftsBusyTimeoutMs is the busy timeout handed to the ftscore engine.
const ftsBusyTimeoutMs = 5000

// Open opens (creating if needed) the database at cfg.Path.
func Open(cfg Config) (*Store, error) {
	if cfg.Path == "" {
		return nil, errors.New("pocketfts: Config.Path is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	writeTimeout := cfg.WriteTimeout
	if writeTimeout <= 0 {
		writeTimeout = DefaultWriteTimeout
	}

	if err := loadFTSLibrary(cfg.Path); err != nil {
		return nil, fmt.Errorf("failed to load FTS library: %w", err)
	}

	db, err := initDB(cfg.Path)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// 寫入走另一個只有一條連線的池子，讓同時進來的寫入在 Go 這側排隊，
	// 而不是在 SQLite 那層搶鎖然後被 busy_timeout 判失敗。
	writeDB, err := initWriteDB(cfg.Path)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize write database: %w", err)
	}

	// 讓 ftscore 的呼叫逾時與寫入時限一致。一筆 upsert 要寫 FTS 索引與 SQL
	// 表；ftscore 內建的預設值是 10 秒，若不對齊，寫入時限設得再大也會被那個
	// 看不見的天花板攔下來。這個設定是 process-wide，搜尋也套用同一個值——
	// 100 萬筆語料上搜尋常見詞的 p99 已經是 8.5 秒，原本的 10 秒本來就太緊。
	setCallTimeout(writeTimeout.Milliseconds())

	engine, err := newFTSEngine(cfg.Path, ftsBusyTimeoutMs, true, ftsOptions{WAL: cfg.FTSWAL})
	if err != nil {
		writeDB.Close()
		db.Close()
		return nil, fmt.Errorf("failed to initialize FTS engine: %w", err)
	}

	setupFTSLogging(logger)

	return &Store{
		db:           db,
		writeDB:      writeDB,
		fts:          engine,
		qe:           newQueryExecutor(db, engine, logger),
		log:          logger,
		writeTimeout: writeTimeout,
	}, nil
}

// Close checkpoints the WAL so the next start finds it empty, then closes the
// full-text engine and both database pools.
func (s *Store) Close() error {
	var errs []error
	if err := checkpointWAL(s.db); err != nil {
		errs = append(errs, err)
	}
	if err := s.fts.Close(); err != nil {
		errs = append(errs, fmt.Errorf("failed to close FTS engine: %w", err))
	}
	if err := s.writeDB.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.db.Close(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// FTSVersion reports the version of the embedded ftscore library.
func (s *Store) FTSVersion() string {
	return getFTSVersion()
}

// WriteTimeout reports the effective write timeout.
func (s *Store) WriteTimeout() time.Duration {
	return s.writeTimeout
}

// execWrite 透過寫入連線池執行一個寫入語句。
//
// 寫入池只有一條連線，所以同時進來的寫入會在 database/sql 的連線池裡排隊，
// 而不是在 SQLite 那層搶鎖。搶鎖會受 busy_timeout 管轄，等太久就直接失敗；
// 排隊則是等待，錯誤變成延遲。但排隊不能無止境，所以整段（等連線加上執行）
// 共用一個 writeTimeout 的期限，超過就回 ErrWriteTimeout。
func (s *Store) execWrite(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, s.writeTimeout)
	defer cancel()

	result, err := s.writeDB.ExecContext(ctx, query, args...)
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w after %s", ErrWriteTimeout, s.writeTimeout)
	}
	return result, err
}

// loadSchemaJSON retrieves a collection's stored schema JSON.
func loadSchemaJSON(ctx context.Context, db *sql.DB, name string) (string, error) {
	var schemaJSON string
	err := db.QueryRowContext(ctx, `SELECT schema_json FROM collections WHERE name = ?;`, name).Scan(&schemaJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("collection '%s' not found", name)
		}
		return "", fmt.Errorf("failed to get schema for collection '%s': %w", name, err)
	}
	return schemaJSON, nil
}

// Schema returns a collection's schema.
func (s *Store) Schema(ctx context.Context, name string) (*CollectionSchema, error) {
	schemaJSON, err := loadSchemaJSON(ctx, s.db, name)
	if err != nil {
		return nil, &NotFoundError{Message: fmt.Sprintf("Collection '%s' not found", name)}
	}
	var schema CollectionSchema
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return nil, errors.New("Failed to parse stored collection schema")
	}
	return &schema, nil
}

// saveSchema saves or updates a collection's schema.
func (s *Store) saveSchema(ctx context.Context, name, schemaJSON string) error {
	_, err := s.execWrite(ctx, `
	INSERT INTO collections (name, schema_json) VALUES (?, ?)
	ON CONFLICT(name) DO UPDATE SET schema_json = excluded.schema_json;
	`, name, schemaJSON)
	if err != nil {
		return fmt.Errorf("failed to save schema for collection '%s': %w", name, err)
	}
	return nil
}

// deleteSchema deletes a collection's schema.
func (s *Store) deleteSchema(ctx context.Context, name string) error {
	res, err := s.execWrite(ctx, `DELETE FROM collections WHERE name = ?;`, name)
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
