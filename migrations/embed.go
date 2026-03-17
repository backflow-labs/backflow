package migrations

import "embed"

// FS contains the embedded goose SQL migrations.
//
//go:embed *.sql
var FS embed.FS
