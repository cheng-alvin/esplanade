package mongo

import (
	"errors"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

var (
	// Indicates the MongoDB connection configuration is invalid or incomplete.
	ErrConfig = errors.New("mongo: configuration error")

	// Indicates no document matched the requested filter.
	ErrNotFound = errors.New("mongo: not found")

	// Indicates a write is a duplicate of another document.
	ErrConflict = errors.New("mongo: conflict")

	// Indicates an ID string could not be parsed as a bson.ObjectID.
	ErrInvalidID = errors.New("mongo: invalid id")
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
