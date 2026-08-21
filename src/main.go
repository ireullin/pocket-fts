package main

import (
	"context"
	"database/sql"
	"embed"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

//go:embed embedded/*.html embedded/*.css embedded/*.js
var staticFS embed.FS

var db *sql.DB
var writeDB *sql.DB
var logger *slog.Logger
var fts *FTS
var queryExecutor *QueryExecutor

// addNoCacheHeaders 添加防快取標頭的中間件
func addNoCacheHeaders(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.Info("Static file request", "path", r.URL.Path, "remote_addr", r.RemoteAddr)
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		handler.ServeHTTP(w, r)
	})
}

func printHelp() {
	fmt.Println("Pocket FTS - Full-Text Search Engine")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  pocket_fts [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -p int              Port to listen on (default: 5122)")
	fmt.Println("  -f string           Database file path (default: \"db.sqlite\")")
	fmt.Println("  -host string        Host address to bind (default: \"localhost\")")
	fmt.Println("  -write-timeout int  Seconds a write (and a search) may take (default: 30)")
	fmt.Println("  -h, --help          Show this help message")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  pocket_fts")
	fmt.Println("  pocket_fts -p 8080 -f /data/my.db")
	fmt.Println("  pocket_fts -p 8080 -f /data/my.db -host 0.0.0.0")
	fmt.Println()
	fmt.Println("Visit http://localhost:5122 after starting the server.")
}

func main() {
	// Define flags
	port := flag.Int("p", 5122, "Port to listen on")
	dbFile := flag.String("f", "db.sqlite", "Database file path")
	host := flag.String("host", "localhost", "Host address to bind")
	showHelp := flag.Bool("h", false, "Show help message")
	writeTimeoutSec := flag.Int("write-timeout", int(DefaultWriteTimeout.Seconds()),
		"Seconds a write may take, covering both the FTS index and the SQL table; also applied to search")
	startupOnly := flag.Bool("startup-only", false, "") // Hidden flag

	// Custom usage message
	flag.Usage = printHelp
	flag.Parse()

	// Handle help flag
	if *showHelp {
		printHelp()
		return
	}

	logFile, err := os.OpenFile("pocket_fts.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer logFile.Close()

	// 使用 MultiWriter 同時寫到檔案和 console
	multiWriter := io.MultiWriter(os.Stdout, logFile)
	logger = slog.New(slog.NewJSONHandler(multiWriter, nil))

	// Load FTS dynamic library (extracts to same directory as database)
	if err := LoadFTSLibrary(*dbFile); err != nil {
		logger.Error("Failed to load FTS library", "error", err)
		os.Exit(1)
	}
	defer UnloadFTSLibrary()
	logger.Info("FTS library loaded successfully")

	db, err = initDB(*dbFile)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// 寫入走另一個只有一條連線的池子，讓同時進來的寫入在 Go 這側排隊，
	// 而不是在 SQLite 那層搶鎖然後被 busy_timeout 判失敗。
	writeDB, err = initWriteDB(*dbFile)
	if err != nil {
		logger.Error("Failed to initialize write database", "error", err)
		os.Exit(1)
	}
	defer writeDB.Close()

	SetWriteTimeout(time.Duration(*writeTimeoutSec) * time.Second)
	logger.Info("Database initialized successfully.", "write_timeout", writeTimeout)

	// 讓 ftscore 的呼叫逾時與寫入時限一致。一筆 upsert 要先寫 FTS 索引再寫
	// SQL 表；ftscore 內建的預設值是 10 秒，若不對齊，-write-timeout 設得再大
	// 也會被那個看不見的天花板攔下來，參數名不符實。這個設定是 process-wide，
	// 搜尋也套用同一個值——100 萬筆語料上搜尋常見詞的 p99 已經是 8.5 秒，
	// 原本的 10 秒本來就太緊。
	SetCallTimeout(writeTimeout.Milliseconds())

	fts, err = NewFTS(*dbFile, 5000, true)
	if err != nil {
		logger.Error("Failed to initialize FTS engine", "error", err)
		os.Exit(1)
	}
	defer fts.Close()
	logger.Info("FTS engine initialized successfully.", "call_timeout", writeTimeout)

	// 設定 FTS C library 的 log callback
	SetupFTSLogging()
	logger.Info("FTS logging setup completed.")

	// 記錄 FTS 版本
	ftsVersion := GetFTSVersion()
	logger.Info("FTS core version", "version", ftsVersion)

	// 初始化查詢執行器
	queryExecutor = NewQueryExecutor(db, fts)
	logger.Info("Query executor initialized successfully.")

	// 提供靜態檔案服務（從 embedded FS，添加防快取標頭）
	staticFiles, err := fs.Sub(staticFS, "embedded")
	if err != nil {
		logger.Error("Failed to load ui page", "error", err)
		os.Exit(1)
	}
	http.Handle("/controller/", addNoCacheHeaders(http.StripPrefix("/controller/", http.FileServer(http.FS(staticFiles)))))

	// 根路徑重導向到管理介面
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/controller/", http.StatusFound)
			return
		}
		fmt.Fprintf(w, "Pocket FTS is running.")
	})
	http.HandleFunc("/collections/create", handleCollectionCreate)
	http.HandleFunc("/collections/delete", handleCollectionDelete)
	http.HandleFunc("/collections/list", handleCollectionList)
	http.HandleFunc("/collections/content", handleCollectionContent)
	http.HandleFunc("/documents/upsert", handleDocumentUpsert)
	http.HandleFunc("/documents/delete", handleDocumentDelete)
	http.HandleFunc("/search", handleSearch)
	http.HandleFunc("/query", handleQuery)

	addr := fmt.Sprintf("%s:%d", *host, *port)
	logger.Info("Server listening", "address", fmt.Sprintf("http://%s", addr))
	logger.Info("Database file", "path", *dbFile)

	if *startupOnly {
		logger.Info("Startup-only mode enabled. Exiting.")
		return
	}

	server := &http.Server{Addr: addr}
	serverErrCh := make(chan error, 1)
	go func() {
		serverErrCh <- server.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM)

	select {
	case err := <-serverErrCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server", "error", err)
			os.Exit(1)
		}
	case <-sigCh:
		// 正常關閉：先停止接受新請求、等進行中的請求做完，時限跟寫入排隊的
		// 時限一致；再把 WAL 清空、主檔案寫到最新，讓下次啟動時 WAL 檔案是
		// 乾淨的。平常運作靠 SQLite 內建的自動 checkpoint，這裡只在正常關閉
		// 時額外做一次。
		logger.Info("Received SIGTERM, shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Error during server shutdown", "error", err)
		}
		if err := checkpointWAL(db); err != nil {
			logger.Error("Failed to checkpoint WAL on shutdown", "error", err)
		}
	}
}
