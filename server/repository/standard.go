package repository

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Repository is the lightweight generic Mongo repository. It requires only an
// ID contract: InsertOne sets _id, and it does not write created_at,
// updated_at, or deleted_at. DeleteOne permanently removes a document.
//
// Use NewAudited when a resource embeds Base or otherwise implements
// Auditable and should receive repository-managed audit fields and soft
// deletes.
type Repository[T any, PT interface {
	*T
	Document
}] struct {
	base *repository[T, PT]
}

// New returns a lightweight Repository backed by collection. timeout bounds
// every individual operation issued through it; pass 0 to use
// DefaultOperationTimeout.
func New[T any, PT interface {
	*T
	Document
}](collection *mongo.Collection, timeout time.Duration) *Repository[T, PT] {
	return &Repository[T, PT]{base: newRepository[T, PT](collection, timeout)}
}
