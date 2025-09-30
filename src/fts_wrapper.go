package main

/*
#cgo LDFLAGS: -L../lib -lftscore
#include <stdlib.h>
#include "../lib/libftscore.h"

// Forward declarations for functions from the .md file that might be missing in the .h file
int FtsDeleteDocument(unsigned long long handle, char* collection_name, char* primary_key_json, char** err);
int FtsDeleteCollection(unsigned long long handle, char* collection_name, char** err);
*/
import "C"
import (
	"errors"
	"unsafe"
)

// FTS is a wrapper around the C FTS engine.
type FTS struct {
	handle C.ulonglong
}

// cToGoError converts a C error string to a Go error, and frees the C string.
func cToGoError(errOut *C.char) error {
	if errOut == nil {
		return errors.New("unknown C error")
	}
	err := errors.New(C.GoString(errOut))
	C.FtsFree(unsafe.Pointer(errOut))
	return err
}

// NewFTS creates a new FTS engine.
func NewFTS(dbPath string, busyTimeoutMs int64, stemming bool) (*FTS, error) {
	cDbPath := C.CString(dbPath)
	defer C.free(unsafe.Pointer(cDbPath))

	var cStemming C.int
	if stemming {
		cStemming = 1
	} else {
		cStemming = 0
	}

	var errOut *C.char
	handle := C.FtsEngineNew(cDbPath, C.longlong(busyTimeoutMs), cStemming, &errOut)

	if handle == 0 {
		return nil, cToGoError(errOut)
	}

	return &FTS{handle: handle}, nil
}

// Close closes the FTS engine.
func (f *FTS) Close() error {
	var errOut *C.char
	ret := C.FtsEngineClose(f.handle, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// CreateCollection creates a new collection.
func (f *FTS) CreateCollection(schemaJSON string) error {
	cSchemaJSON := C.CString(schemaJSON)
	defer C.free(unsafe.Pointer(cSchemaJSON))

	var errOut *C.char
	ret := C.FtsCreateCollection(f.handle, cSchemaJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// UpsertDocument upserts a document into a collection.
func (f *FTS) UpsertDocument(collectionName, documentJSON string) error {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cDocumentJSON := C.CString(documentJSON)
	defer C.free(unsafe.Pointer(cDocumentJSON))

	var errOut *C.char
	ret := C.FtsUpsertDocument(f.handle, cCollectionName, cDocumentJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// Search performs a search query.
func (f *FTS) Search(collectionName, requestJSON string) (string, error) {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cRequestJSON := C.CString(requestJSON)
	defer C.free(unsafe.Pointer(cRequestJSON))

	var resultOut *C.char
	var errOut *C.char
	ret := C.FtsSearch(f.handle, cCollectionName, cRequestJSON, &resultOut, &errOut)

	if ret != 0 {
		if resultOut != nil {
			C.FtsFree(unsafe.Pointer(resultOut))
		}
		return "", cToGoError(errOut)
	}

	if errOut != nil {
		if resultOut != nil {
			C.FtsFree(unsafe.Pointer(resultOut))
		}
		return "", cToGoError(errOut)
	}
	
	result := C.GoString(resultOut)
	C.FtsFree(unsafe.Pointer(resultOut))

	return result, nil
}

// DeleteCollection deletes a collection and its associated FTS index.
func (f *FTS) DeleteCollection(name string) error {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	var errOut *C.char
	ret := C.FtsDeleteCollection(f.handle, cName, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}

// DeleteDocument deletes a document from the FTS index.
func (f *FTS) DeleteDocument(collectionName, primaryKeyJSON string) error {
	cCollectionName := C.CString(collectionName)
	defer C.free(unsafe.Pointer(cCollectionName))
	cPrimaryKeyJSON := C.CString(primaryKeyJSON)
	defer C.free(unsafe.Pointer(cPrimaryKeyJSON))

	var errOut *C.char
	ret := C.FtsDeleteDocument(f.handle, cCollectionName, cPrimaryKeyJSON, &errOut)
	if ret != 0 {
		return cToGoError(errOut)
	}
	return nil
}