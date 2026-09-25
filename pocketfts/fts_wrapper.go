package pocketfts

/*
#include <stdlib.h>

typedef void (*fts_log_cb_t)(int level, const char* message, void* user_data);

// C callback function that will forward to Go
extern void logCallback(int level, char* message, void* user_data);
*/
import "C"
import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"unsafe"
)

// ftsEngine is a wrapper around the C FTS engine.
type ftsEngine struct {
	handle C.ulonglong
}

// cToGoError converts a C error string to a Go error, and frees the C string.
func cToGoError(errOut *C.char) error {
	if errOut == nil {
		return errors.New("unknown C error")
	}
	err := errors.New(C.GoString(errOut))
	callFtsFree(unsafe.Pointer(errOut))
	return err
}

// ftsOptions carries the optional ftscore engine settings.
type ftsOptions struct {
	WAL bool `json:"wal"`
}

// newFTSEngine creates a new FTS engine with the supplied options.
func newFTSEngine(dbPath string, busyTimeoutMs int64, stemming bool, opts ftsOptions) (*ftsEngine, error) {
	cDbPath := C.CString(dbPath)
	defer C.free(unsafe.Pointer(cDbPath))

	optionsJSON, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal ftscore options: %w", err)
	}
	cOptions := C.CString(string(optionsJSON))
	defer C.free(unsafe.Pointer(cOptions))

	var cStemming C.int
	if stemming {
		cStemming = 1
	} else {
		cStemming = 0
	}

	var errOut *C.char
	handle := callFtsEngineNewWithOptions(cDbPath, C.longlong(busyTimeoutMs), cStemming, cOptions, &errOut)

	if handle == 0 {
		return nil, cToGoError(errOut)
	}

	return &ftsEngine{handle: handle}, nil
}

// Close closes the FTS engine.
func (f *ftsEngine) Close() error {
	var errOut *C.char
	ret := callFtsEngineClose(f.handle, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// CreateCollection creates a new collection.
func (f *ftsEngine) CreateCollection(schemaJSON string) error {
	cSchemaJSON := C.CString(schemaJSON)
	defer C.free(unsafe.Pointer(cSchemaJSON))

	var errOut *C.char
	ret := callFtsCreateCollection(f.handle, cSchemaJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// UpsertDocument upserts a document into a collection.
func (f *ftsEngine) UpsertDocument(collectionName, documentJSON string) error {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cDocumentJSON := C.CString(documentJSON)
	defer C.free(unsafe.Pointer(cDocumentJSON))

	var errOut *C.char
	ret := callFtsUpsertDocument(f.handle, cCollectionName, cDocumentJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// Search performs a search query.
func (f *ftsEngine) Search(collectionName, requestJSON string) (string, error) {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cRequestJSON := C.CString(requestJSON)
	defer C.free(unsafe.Pointer(cRequestJSON))

	var resultOut *C.char
	var errOut *C.char
	ret := callFtsSearch(f.handle, cCollectionName, cRequestJSON, &resultOut, &errOut)

	if ret != 0 {
		if resultOut != nil {
			callFtsFree(unsafe.Pointer(resultOut))
		}
		return "", cToGoError(errOut)
	}

	if errOut != nil {
		if resultOut != nil {
			callFtsFree(unsafe.Pointer(resultOut))
		}
		return "", cToGoError(errOut)
	}

	result := C.GoString(resultOut)
	callFtsFree(unsafe.Pointer(resultOut))

	return result, nil
}

// DeleteCollection deletes a collection and its associated FTS index.
func (f *ftsEngine) DeleteCollection(name string) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var errOut *C.char
	ret := callFtsDeleteCollection(f.handle, cName, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// DeleteDocument deletes a document from the FTS index.
func (f *ftsEngine) DeleteDocument(collectionName, primaryKeyJSON string) error {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cPrimaryKeyJSON := C.CString(primaryKeyJSON)
	defer C.free(unsafe.Pointer(cPrimaryKeyJSON))

	var errOut *C.char
	ret := callFtsDeleteDocument(f.handle, cCollectionName, cPrimaryKeyJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// setCallTimeout adjusts the default call timeout (in milliseconds) that the
// FTS C library applies to FtsUpsertDocument/FtsDeleteDocument/FtsSearch. It
// is process-wide (not tied to a specific *FTS handle). Passing <= 0
// disables the timeout entirely.
func setCallTimeout(ms int64) {
	callFtsSetCallTimeout(C.longlong(ms))
}

// getFTSVersion returns the FTS core version string.
func getFTSVersion() string {
	versionCStr := callFtsVersion()
	if versionCStr == nil {
		return "unknown"
	}
	version := C.GoString(versionCStr)
	callFtsFree(unsafe.Pointer(versionCStr))
	return version
}

// ftsLogger receives the ftscore library's own log lines. The library's log
// callback is process-wide, so the most recently opened Store's logger wins.
var ftsLogger atomic.Pointer[slog.Logger]

// setupFTSLogging points the FTS C library's log callback at logger.
func setupFTSLogging(logger *slog.Logger) {
	ftsLogger.Store(logger)
	callFtsSetLogCallback((C.fts_log_cb_t)(unsafe.Pointer(C.logCallback)), nil)
}

//export logCallback
func logCallback(level C.int, message *C.char, userData unsafe.Pointer) {
	logger := ftsLogger.Load()
	if logger == nil {
		return
	}

	msg := C.GoString(message)

	switch int(level) {
	case 0: // Debug
		logger.Debug("FTS: " + msg)
	case 1: // Info
		logger.Info("FTS: " + msg)
	case 2: // Warn
		logger.Warn("FTS: " + msg)
	case 3: // Error
		logger.Error("FTS: " + msg)
	default:
		logger.Info("FTS: " + msg)
	}
}
