package main

import (
	"context"
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

	"github.com/ireullin/pocket-fts/pocketfts"
)

//go:embed embedded/*.html embedded/*.css embedded/*.js
var staticFS embed.FS

var logger *slog.Logger
var store *pocketfts.Store

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
	writeTimeoutSec := flag.Int("write-timeout", int(pocketfts.DefaultWriteTimeout.Seconds()),
		"Seconds a write may take, covering both the FTS index and the SQL table; also applied to search")
	startupOnly := flag.Bool("startup-only", false, "") // Hidden flag
	ftsWAL := flag.Bool("fts-wal", false, "")           // Hidden flag

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

	store, err = pocketfts.Open(pocketfts.Config{
		Path:         *dbFile,
		WriteTimeout: time.Duration(*writeTimeoutSec) * time.Second,
		Logger:       logger,
		FTSWAL:       *ftsWAL,
	})
	if err != nil {
		logger.Error("Failed to open store", "error", err, "fts_wal", *ftsWAL)
		os.Exit(1)
	}
	defer func() {
		if err := store.Close(); err != nil {
			logger.Error("Failed to close store", "error", err)
		}
	}()
	logger.Info("Store opened successfully.", "write_timeout", store.WriteTimeout(), "fts_wal", *ftsWAL)

	// 記錄 FTS 版本
	logger.Info("FTS core version", "version", store.FTSVersion())

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
	http.HandleFunc("/documents/update", handleDocumentUpdate)
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
		// 時限一致；接著 defer 的 store.Close 會把 WAL 清空、主檔案寫到最新，
		// 讓下次啟動時 WAL 檔案是乾淨的。
		logger.Info("Received SIGTERM, shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), store.WriteTimeout())
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("Error during server shutdown", "error", err)
		}
	}
}
