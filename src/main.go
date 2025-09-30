package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
)

var db *sql.DB
var logger *slog.Logger
var fts *FTS

func main() {
	logFile, err := os.OpenFile("pocket_fts.log", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer logFile.Close()
	logger = slog.New(slog.NewJSONHandler(logFile, nil))

	port := flag.Int("p", 5122, "Port to listen on")
	dbFile := flag.String("f", "db.sqlite", "Database file path")
	startupOnly := flag.Bool("startup-only", false, "Run startup logic only and then exit.")
	flag.Parse()

	db, err = initDB(*dbFile)
	if err != nil {
		logger.Error("Failed to initialize database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("Database initialized successfully.")

	fts, err = NewFTS(*dbFile, 5000, true)
	if err != nil {
		logger.Error("Failed to initialize FTS engine", "error", err)
		os.Exit(1)
	}
	defer fts.Close()
	logger.Info("FTS engine initialized successfully.")

	// Register API handlers
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Pocket FTS is running.")
	})
	http.HandleFunc("/collections/create", handleCollectionCreate)
	http.HandleFunc("/collections/delete", handleCollectionDelete)
	http.HandleFunc("/documents/upsert", handleDocumentUpsert)
	http.HandleFunc("/documents/delete", handleDocumentDelete)
	http.HandleFunc("/search", handleSearch)

	addr := fmt.Sprintf(":%d", *port)
	logger.Info("Server listening", "address", fmt.Sprintf("http://localhost%s", addr))
	logger.Info("Database file", "path", *dbFile)

	if *startupOnly {
		logger.Info("Startup-only mode enabled. Exiting.")
		return
	}

	err = http.ListenAndServe(addr, nil)
	if err != nil {
		logger.Error("Failed to start server", "error", err)
		os.Exit(1)
	}
}
