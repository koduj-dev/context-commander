// Package migrations embeds the golang-migrate SQL files into the ccd
// binary itself, so `ccd migrate` (and the integration test suite) don't
// need the migrations/ directory to exist on disk at runtime: ccd is one
// binary that is the server, the migrator and the CLI (see
// docs/architecture-cookbook.md §21).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

// PgvectorFS is the optional migration set for plaintext pgvector storage
// of memory vectors (docs/adr/0028). It is not part of FS on purpose: the
// core sequence must apply on a Postgres without the pgvector extension, so
// this set is applied only by `ccd migrate --pgvector` and tracked in a
// version table of its own.
//
//go:embed pgvector/*.sql
var PgvectorFS embed.FS
