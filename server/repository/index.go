package repository

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// IndexProvider is implemented by resource packages that need indexes on
// their own collection. Index definitions should live next to the
// repository that owns them — e.g. as an Indexes() function in the same
// package as a future devices.Device type — not in a separate,
// untracked location, so schema and access code can't drift apart.
type IndexProvider interface {
	// CollectionName is the Mongo collection the indexes below apply to.
	CollectionName() string

	// Indexes returns the index models to ensure exist on that
	// collection.
	Indexes() []mongo.IndexModel
}

// EnsureIndexes creates the indexes declared by each provider. It is
// idempotent — creating an index that already exists with matching
// options is a no-op — so it's safe to call on every startup, once,
// after mongo.New.
//
// Called with no providers, EnsureIndexes is a no-op; resource packages
// register themselves here as they're built out.
func EnsureIndexes(ctx context.Context, db *mongo.Database, providers ...IndexProvider) error {
	for _, p := range providers {
		models := p.Indexes()
		if len(models) == 0 {
			continue
		}

		opCtx, cancel := context.WithTimeout(ctx, DefaultOperationTimeout)
		_, err := db.Collection(p.CollectionName()).Indexes().CreateMany(opCtx, models)
		cancel()
		if err != nil {
			return fmt.Errorf("repository: ensuring indexes for %q: %w", p.CollectionName(), err)
		}
	}
	return nil
}
