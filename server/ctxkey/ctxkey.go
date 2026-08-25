// Package ctxkey defines unexported context-key types used across the
// server to avoid key collisions.
package ctxkey

type key int

const (
	// RequestID is the context key for the per-request unique identifier.
	RequestID key = iota
)
