// Package migrations embeds the golang-migrate SQL files into the ccd
// binary itself, so `ccd migrate` (and the integration test suite) don't
// need the migrations/ directory to exist on disk at runtime — see
// docs/architecture-cookbook.md §21 ("cmd/ccd: jediný binary: server +
// migrace + CLI vstupní bod").
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
