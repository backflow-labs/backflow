package migrations

import "embed"

// FS exposes embedded goose migration files.
//
//go:embed *.sql
var FS embed.FS
