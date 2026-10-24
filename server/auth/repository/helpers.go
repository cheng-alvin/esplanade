package repository

import (
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// first returns the first document, passing through query errors and mapping
// an empty result to esmongo.ErrNotFound.
func first[PT any](items []PT, err error) (PT, error) {
	if err != nil {
		var zero PT
		return zero, err
	}
	if len(items) == 0 {
		var zero PT
		return zero, esmongo.ErrNotFound
	}
	return items[0], nil
}

// objectID parses a hex id, mapping malformed input to esmongo.ErrNotFound.
func objectID(id string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.ObjectID{}, esmongo.ErrNotFound
	}
	return oid, nil
}
