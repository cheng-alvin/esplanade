package repository

import (
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

// Auditable is implemented by documents that support repository-managed audit
// timestamps. It embeds Document, so every auditable document also satisfies
// the ID contract required by repository operations.
type Auditable interface {
	Document
	SetCreatedAt(time.Time)
	SetUpdatedAt(time.Time)
}

// AuditedDocument is a descriptive alias for Auditable.
type AuditedDocument = Auditable

type Base struct {
	ObjectID  bson.ObjectID `bson:"_id,omitempty"`
	CreatedAt time.Time     `bson:"created_at"`
	UpdatedAt time.Time     `bson:"updated_at"`
	DeletedAt *time.Time    `bson:"deleted_at,omitempty"`
}

func (b *Base) SetID(id bson.ObjectID)   { b.ObjectID = id }
func (b *Base) ID() bson.ObjectID        { return b.ObjectID }
func (b *Base) SetCreatedAt(t time.Time) { b.CreatedAt = t }
func (b *Base) SetUpdatedAt(t time.Time) { b.UpdatedAt = t }

// AuditedRepository is the audit-aware generic Mongo repository. It requires
// an Auditable document and manages created_at and updated_at on inserts,
// updated_at on updates, and deleted_at on soft deletes. Reads and updates
// exclude soft-deleted documents automatically.
type AuditedRepository[T any, PT interface {
	*T
	Auditable
}] struct {
	base *repository[T, PT]
}

// NewAudited returns an audit-aware repository backed by collection. timeout
// bounds every individual operation issued through it; pass 0 to use
// DefaultOperationTimeout.
func NewAudited[T any, PT interface {
	*T
	Auditable
}](collection *mongo.Collection, timeout time.Duration) *AuditedRepository[T, PT] {
	return &AuditedRepository[T, PT]{base: newRepository[T, PT](collection, timeout)}
}
