package migrations

import "embed"

// Files exposes the goose SQL migrations for embedding-aware callers.
//
//go:embed *.sql
var Files embed.FS
