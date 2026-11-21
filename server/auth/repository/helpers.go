package repository

import (
	esmongo "github.com/cheng-alvin/esplanade/server/db/mongo"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// objectID parses a hex id, mapping malformed input to esmongo.ErrNotFound.
func objectID(id string) (bson.ObjectID, error) {
	oid, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return bson.ObjectID{}, esmongo.ErrNotFound
	}
	return oid, nil
}

// scoped excludes soft-deleted documents from an audited resource's filter.
func scoped(filter bson.M) bson.M {
	filter["deleted_at"] = bson.M{"$exists": false}
	return filter
}
