package mongo

import (
	"errors"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Sentinel errors returned by the repository layer. Callers (ultimately
// the handler layer, via the respond package) map these to HTTP status
// codes without ever needing to import or type-assert against the
// driver's own error types.
var (
	// ErrNotFound indicates no document matched the requested filter.
	ErrNotFound = errors.New("mongo: not found")

	// ErrConflict indicates a write violated a uniqueness constraint
	// (duplicate key).
	ErrConflict = errors.New("mongo: conflict")
)

// TranslateError maps a raw driver error into one of the sentinels
// above where possible. Errors that don't match a known case are
// returned unchanged, so callers can still errors.Is/As against
// anything TranslateError doesn't recognize.
func TranslateError(err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, mongo.ErrNoDocuments) {
		return ErrNotFound
	}

	if mongo.IsDuplicateKeyError(err) {
		return ErrConflict
	}

	return err
}
