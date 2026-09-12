package database

import "embed"

// Migrations holds the numbered SQL migration files,
// compiled into the binary.
//
// Embedding them means the schema can be applied without
// the process needing to know where the source tree is.
// The tests rely on this: a test's working directory is
// its own package directory, so a path like
// "../database/migrations" would resolve differently
// depending on which package is running.
//
//go:embed migrations/*.sql
var Migrations embed.FS
