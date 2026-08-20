package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// TestWritePoolIsSingleConnection 確認寫入池只開一條連線。
//
// SQLite 同一時間只允許一個寫入者。若讓多條連線去搶鎖，搶輸的會在
// busy_timeout 到期後回傳 SQLITE_BUSY，呼叫端收到錯誤，寫入等於被丟掉。
// 限制成一條連線之後，同時進來的寫入改在 database/sql 的連線池裡排隊。
func TestWritePoolIsSingleConnection(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	readDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer readDB.Close()

	writer, err := initWriteDB(dbPath)
	if err != nil {
		t.Fatalf("initWriteDB failed: %v", err)
	}
	defer writer.Close()

	if got := writer.Stats().MaxOpenConnections; got != 1 {
		t.Fatalf("write pool allows %d connections, want 1", got)
	}
}

// TestReadPoolIsNotLimited 確認讀取池沒有被限制成一條連線。
//
// 純 SQL 讀取是唯一能隨核心數成長的路徑。把讀取一併限制成一條連線，
// 範圍查詢的吞吐量會掉到三分之一。
func TestReadPoolIsNotLimited(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	readDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer readDB.Close()

	if got := readDB.Stats().MaxOpenConnections; got == 1 {
		t.Fatal("read pool is limited to a single connection; reads would lose their concurrency")
	}
}

// TestExecWriteTimesOutWhileQueued 確認排隊不是無止境的。
//
// 寫入池只有一條連線，所以只要有人佔著它，後續的寫入就得等。等待有時限，
// 超過就回 ErrWriteTimeout，handler 據此回 503。
func TestExecWriteTimesOutWhileQueued(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	readDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer readDB.Close()
	db = readDB

	writer, err := initWriteDB(dbPath)
	if err != nil {
		t.Fatalf("initWriteDB failed: %v", err)
	}
	defer writer.Close()
	writeDB = writer

	original := writeTimeout
	t.Cleanup(func() { writeTimeout = original })
	SetWriteTimeout(200 * time.Millisecond)

	// 開一個交易佔住那條唯一的寫入連線。
	tx, err := writer.Begin()
	if err != nil {
		t.Fatalf("failed to begin holder tx: %v", err)
	}
	defer tx.Rollback()

	start := time.Now()
	_, err = execWrite("INSERT INTO collections (name, schema_json) VALUES ('queued', '{}')")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected the queued write to time out, got success")
	}
	if !errors.Is(err, ErrWriteTimeout) {
		t.Fatalf("got %v, want an error wrapping ErrWriteTimeout", err)
	}
	if elapsed < 150*time.Millisecond {
		t.Fatalf("gave up after %v; it should have waited out the timeout", elapsed)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("waited %v; the timeout was not applied", elapsed)
	}
}

// TestExecWriteSucceedsWhenPoolIsFree 確認沒有排隊時寫入照常成功。
func TestExecWriteSucceedsWhenPoolIsFree(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")

	readDB, err := initDB(dbPath)
	if err != nil {
		t.Fatalf("initDB failed: %v", err)
	}
	defer readDB.Close()
	db = readDB

	writer, err := initWriteDB(dbPath)
	if err != nil {
		t.Fatalf("initWriteDB failed: %v", err)
	}
	defer writer.Close()
	writeDB = writer

	if _, err := execWrite("INSERT INTO collections (name, schema_json) VALUES ('ok', '{}')"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var count int
	if err := readDB.QueryRow("SELECT COUNT(*) FROM collections WHERE name = 'ok'").Scan(&count); err != nil {
		t.Fatalf("failed to read back: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d rows, want 1", count)
	}
}

// TestSetWriteTimeoutIgnoresNonPositive 確認非正值不會把時限歸零。
func TestSetWriteTimeoutIgnoresNonPositive(t *testing.T) {
	original := writeTimeout
	t.Cleanup(func() { writeTimeout = original })

	SetWriteTimeout(5 * time.Second)
	SetWriteTimeout(0)
	SetWriteTimeout(-1 * time.Second)

	if writeTimeout != 5*time.Second {
		t.Fatalf("write timeout is %s, want 5s", writeTimeout)
	}
}

// TestDefaultWriteTimeout 確認預設值是 30 秒。
func TestDefaultWriteTimeout(t *testing.T) {
	if DefaultWriteTimeout != 30*time.Second {
		t.Fatalf("default write timeout is %s, want 30s", DefaultWriteTimeout)
	}
}

// TestIsTimeoutError 確認兩種來源的逾時都認得出來。
//
// SQL 側回傳的是 ErrWriteTimeout；FTS 側的錯誤來自 ftscore 這個 C 動態庫，
// 跨過 cgo 邊界之後只剩字串，只能認內容。兩者都要對應到 503。
func TestIsTimeoutError(t *testing.T) {
	if !isTimeoutError(fmt.Errorf("%w after 30s", ErrWriteTimeout)) {
		t.Fatal("wrapped ErrWriteTimeout should be recognised as a timeout")
	}
	if !isTimeoutError(errors.New("insert into docs: context deadline exceeded")) {
		t.Fatal("the ftscore timeout string should be recognised as a timeout")
	}
	if isTimeoutError(errors.New("no such column: nope")) {
		t.Fatal("an ordinary error must not be treated as a timeout")
	}
	if isTimeoutError(nil) {
		t.Fatal("nil must not be treated as a timeout")
	}
}
