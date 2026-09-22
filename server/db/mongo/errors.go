package mongo

import (
	"errors"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	// Indicates no document matched the requested filter.
	ErrNotFound = errors.New("mongo: not found")

	// Indicates a write is a duplicate of another document.
	ErrConflict = errors.New("mongo: conflict")
)

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
