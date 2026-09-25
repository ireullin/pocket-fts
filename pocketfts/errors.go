package pocketfts

import (
	"errors"
	"strings"
)

// NotFoundError means the collection (or document) named in the call does
// not exist. The HTTP layer maps it to 404.
type NotFoundError struct {
	Message string
}

func (e *NotFoundError) Error() string {
	return e.Message
}

// IsValidation reports whether err means the request itself is invalid.
func IsValidation(err error) bool {
	var target *ValidationError
	return errors.As(err, &target)
}

// IsNotFound reports whether err means the named collection or document does
// not exist.
func IsNotFound(err error) bool {
	var target *NotFoundError
	return errors.As(err, &target)
}

// IsTimeout reports whether err means a write ran out of time. That is the
// server being saturated, not a bad request; retrying later can succeed.
func IsTimeout(err error) bool {
	return errors.Is(err, ErrWriteTimeout)
}

// isTimeoutError 判斷錯誤是不是逾時。
//
// SQL 側走 execWrite，回傳的是 ErrWriteTimeout。FTS 側的錯誤來自 ftscore 這個
// C 動態庫，跨越 cgo 邊界之後只剩下字串，沒有型別可以比對，只能認它的內容。
func isTimeoutError(err error) bool {
	if errors.Is(err, ErrWriteTimeout) {
		return true
	}
	return err != nil && strings.Contains(err.Error(), "context deadline exceeded")
}
