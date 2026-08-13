package main

/*
#cgo LDFLAGS: -ldl
#include <stdlib.h>
#include <dlfcn.h>

typedef void (*fts_log_cb_t)(int level, const char* message, void* user_data);

typedef unsigned long long (*fts_engine_new_t)(char*, long long, int, char**);
typedef int (*fts_engine_close_t)(unsigned long long, char**);
typedef int (*fts_create_collection_t)(unsigned long long, char*, char**);
typedef int (*fts_upsert_document_t)(unsigned long long, char*, char*, char**);
typedef int (*fts_delete_document_t)(unsigned long long, char*, char*, char**);
typedef int (*fts_delete_collection_t)(unsigned long long, char*, char**);
typedef int (*fts_search_t)(unsigned long long, char*, char*, char**, char**);
typedef void (*fts_free_t)(void*);
typedef void (*fts_set_log_callback_t)(fts_log_cb_t, void*);
typedef char* (*fts_version_t)(void);
typedef void (*fts_set_call_timeout_t)(long long);

static void* g_lib_handle = NULL;
static fts_engine_new_t g_fts_engine_new = NULL;
static fts_engine_close_t g_fts_engine_close = NULL;
static fts_create_collection_t g_fts_create_collection = NULL;
static fts_upsert_document_t g_fts_upsert_document = NULL;
static fts_delete_document_t g_fts_delete_document = NULL;
static fts_delete_collection_t g_fts_delete_collection = NULL;
static fts_search_t g_fts_search = NULL;
static fts_free_t g_fts_free = NULL;
static fts_set_log_callback_t g_fts_set_log_callback = NULL;
static fts_version_t g_fts_version = NULL;
static fts_set_call_timeout_t g_fts_set_call_timeout = NULL;

static int load_fts_library(const char* lib_path, char** err_msg) {
    g_lib_handle = dlopen(lib_path, RTLD_NOW | RTLD_GLOBAL);
    if (!g_lib_handle) {
        *err_msg = (char*)dlerror();
        return -1;
    }

    dlerror();
    g_fts_engine_new = (fts_engine_new_t)dlsym(g_lib_handle, "FtsEngineNew");
    if (!g_fts_engine_new) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_engine_close = (fts_engine_close_t)dlsym(g_lib_handle, "FtsEngineClose");
    if (!g_fts_engine_close) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_create_collection = (fts_create_collection_t)dlsym(g_lib_handle, "FtsCreateCollection");
    if (!g_fts_create_collection) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_upsert_document = (fts_upsert_document_t)dlsym(g_lib_handle, "FtsUpsertDocument");
    if (!g_fts_upsert_document) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_delete_document = (fts_delete_document_t)dlsym(g_lib_handle, "FtsDeleteDocument");
    if (!g_fts_delete_document) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_delete_collection = (fts_delete_collection_t)dlsym(g_lib_handle, "FtsDeleteCollection");
    if (!g_fts_delete_collection) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_search = (fts_search_t)dlsym(g_lib_handle, "FtsSearch");
    if (!g_fts_search) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_free = (fts_free_t)dlsym(g_lib_handle, "FtsFree");
    if (!g_fts_free) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_set_log_callback = (fts_set_log_callback_t)dlsym(g_lib_handle, "FtsSetLogCallback");
    if (!g_fts_set_log_callback) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_version = (fts_version_t)dlsym(g_lib_handle, "FtsVersion");
    if (!g_fts_version) { *err_msg = (char*)dlerror(); return -1; }

    g_fts_set_call_timeout = (fts_set_call_timeout_t)dlsym(g_lib_handle, "FtsSetCallTimeout");
    if (!g_fts_set_call_timeout) { *err_msg = (char*)dlerror(); return -1; }

    return 0;
}

static int unload_fts_library() {
    if (g_lib_handle) {
        int ret = dlclose(g_lib_handle);
        g_lib_handle = NULL;
        return ret;
    }
    return 0;
}

static unsigned long long call_fts_engine_new(char* db_path, long long timeout, int stemming, char** err_out) {
    return g_fts_engine_new ? g_fts_engine_new(db_path, timeout, stemming, err_out) : 0;
}

static int call_fts_engine_close(unsigned long long handle, char** err_out) {
    return g_fts_engine_close ? g_fts_engine_close(handle, err_out) : -1;
}

static int call_fts_create_collection(unsigned long long handle, char* schema_json, char** err_out) {
    return g_fts_create_collection ? g_fts_create_collection(handle, schema_json, err_out) : -1;
}

static int call_fts_upsert_document(unsigned long long handle, char* collection, char* document, char** err_out) {
    return g_fts_upsert_document ? g_fts_upsert_document(handle, collection, document, err_out) : -1;
}

static int call_fts_delete_document(unsigned long long handle, char* collection, char* primary_key, char** err_out) {
    return g_fts_delete_document ? g_fts_delete_document(handle, collection, primary_key, err_out) : -1;
}

static int call_fts_delete_collection(unsigned long long handle, char* collection, char** err_out) {
    return g_fts_delete_collection ? g_fts_delete_collection(handle, collection, err_out) : -1;
}

static int call_fts_search(unsigned long long handle, char* collection, char* request, char** result_out, char** err_out) {
    return g_fts_search ? g_fts_search(handle, collection, request, result_out, err_out) : -1;
}

static void call_fts_free(void* ptr) {
    if (g_fts_free) g_fts_free(ptr);
}

static void call_fts_set_log_callback(fts_log_cb_t cb, void* user_data) {
    if (g_fts_set_log_callback) g_fts_set_log_callback(cb, user_data);
}

static char* call_fts_version() {
    return g_fts_version ? g_fts_version() : NULL;
}

static void call_fts_set_call_timeout(long long ms) {
    if (g_fts_set_call_timeout) g_fts_set_call_timeout(ms);
}
*/
import "C"
import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"
)

//go:embed embedded/ftscore
var embeddedLibrary []byte

func LoadFTSLibrary(dbPath string) error {
	// Get absolute path of database
	absDbPath, err := filepath.Abs(dbPath)
	if err != nil {
		return fmt.Errorf("failed to resolve database path: %w", err)
	}

	// Generate library path: same name as db but with .indices2 extension
	dbDir := filepath.Dir(absDbPath)
	dbBase := filepath.Base(absDbPath)
	dbNameWithoutExt := dbBase[:len(dbBase)-len(filepath.Ext(dbBase))]
	libPath := filepath.Join(dbDir, dbNameWithoutExt+".indices2")

	// Write embedded library to disk
	if err := os.WriteFile(libPath, embeddedLibrary, 0755); err != nil {
		return fmt.Errorf("failed to extract library: %w", err)
	}

	// Load the library
	cPath := C.CString(libPath)
	defer C.free(unsafe.Pointer(cPath))

	var errMsg *C.char
	if ret := C.load_fts_library(cPath, &errMsg); ret != 0 {
		if errMsg != nil {
			return fmt.Errorf("failed to load library %s: %s", libPath, C.GoString(errMsg))
		}
		return fmt.Errorf("failed to load library %s", libPath)
	}
	return nil
}

func UnloadFTSLibrary() error {
	if ret := C.unload_fts_library(); ret != 0 {
		return fmt.Errorf("failed to unload library")
	}
	return nil
}

func callFtsEngineNew(dbPath *C.char, busyTimeoutMs C.longlong, stemming C.int, errOut **C.char) C.ulonglong {
	return C.call_fts_engine_new(dbPath, busyTimeoutMs, stemming, errOut)
}

func callFtsEngineClose(handle C.ulonglong, errOut **C.char) C.int {
	return C.call_fts_engine_close(handle, errOut)
}

func callFtsCreateCollection(handle C.ulonglong, schemaJSON *C.char, errOut **C.char) C.int {
	return C.call_fts_create_collection(handle, schemaJSON, errOut)
}

func callFtsUpsertDocument(handle C.ulonglong, collectionName *C.char, documentJSON *C.char, errOut **C.char) C.int {
	return C.call_fts_upsert_document(handle, collectionName, documentJSON, errOut)
}

func callFtsDeleteDocument(handle C.ulonglong, collectionName *C.char, primaryKeyJSON *C.char, errOut **C.char) C.int {
	return C.call_fts_delete_document(handle, collectionName, primaryKeyJSON, errOut)
}

func callFtsDeleteCollection(handle C.ulonglong, collectionName *C.char, errOut **C.char) C.int {
	return C.call_fts_delete_collection(handle, collectionName, errOut)
}

func callFtsSearch(handle C.ulonglong, collectionName *C.char, requestJSON *C.char, resultOut **C.char, errOut **C.char) C.int {
	return C.call_fts_search(handle, collectionName, requestJSON, resultOut, errOut)
}

func callFtsFree(ptr unsafe.Pointer) {
	C.call_fts_free(ptr)
}

func callFtsSetLogCallback(cb C.fts_log_cb_t, userData unsafe.Pointer) {
	C.call_fts_set_log_callback(cb, userData)
}

func callFtsVersion() *C.char {
	return C.call_fts_version()
}

func callFtsSetCallTimeout(ms C.longlong) {
	C.call_fts_set_call_timeout(ms)
}

