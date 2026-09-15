// Package mongo provides MongoDB connection lifecycle management for the
// Esplanade server: client construction, health checks, and graceful
// shutdown.
//
// Resource-specific query logic does not belong here — see the
// repository package for the generic CRUD layer that sits on top of a
// connected client, and per-resource packages for anything
// domain-specific.
package mongo

import (
	"context"
	"fmt"
	"strings"

	"github.com/cheng-alvin/esplanade/server/config"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
)

// New builds a MongoDB client using the connection, pool, and timeout
// settings from cfg, and confirms connectivity with a bounded ping
// before returning.
//
// In production, cfg.MongoURI must specify TLS — New fails fast rather
// than silently connecting in the clear.
func New(ctx context.Context, cfg *config.Config) (*mongo.Client, error) {
	if cfg.MongoURI == "" {
		return nil, fmt.Errorf("mongo: MongoURI is not configured")
	}

	if cfg.Env == "production" && !hasTLS(cfg.MongoURI) {
		return nil, fmt.Errorf("mongo: production environment requires a TLS-enabled connection URI")
	}

	clientOpts := options.Client().
		ApplyURI(cfg.MongoURI).
		SetConnectTimeout(cfg.MongoConnectTimeout).
		SetServerSelectionTimeout(cfg.MongoServerSelectionTimeout).
		SetMaxPoolSize(cfg.MongoMaxPoolSize).
		SetMinPoolSize(cfg.MongoMinPoolSize).
		SetRetryWrites(true)

	client, err := mongo.Connect(clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo: connecting client: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.MongoConnectTimeout)
	defer cancel()

	if err := Ping(pingCtx, client); err != nil {
		_ = client.Disconnect(ctx)
		return nil, fmt.Errorf("mongo: initial ping failed: %w", err)
	}

	return client, nil
}

// Ping performs a short, bounded health check against the primary. It
// is safe to call frequently, e.g. from a /readyz handler.
func Ping(ctx context.Context, client *mongo.Client) error {
	return client.Ping(ctx, readpref.Primary())
}

// Disconnect closes the client's connections and releases its
// resources. It should be called exactly once, from main.go's shutdown
// sequence, alongside srv.Shutdown.
func Disconnect(ctx context.Context, client *mongo.Client) error {
	if client == nil {
		return nil
	}
	return client.Disconnect(ctx)
}

// Database returns a handle to the named database on client.
func Database(client *mongo.Client, name string) *mongo.Database {
	return client.Database(name)
}

// Collection returns a handle to the named collection within db.
func Collection(db *mongo.Database, name string) *mongo.Collection {
	return db.Collection(name)
}

// hasTLS reports whether uri appears to establish a TLS connection.
// mongodb+srv:// implies TLS by default; a plain mongodb:// URI requires
// an explicit tls=true (or the legacy ssl=true) query parameter.
func hasTLS(uri string) bool {
	if strings.HasPrefix(uri, "mongodb+srv://") {
		return true
	}
	lower := strings.ToLower(uri)
	return strings.Contains(lower, "tls=true") || strings.Contains(lower, "ssl=true")
}
