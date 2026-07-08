// Package migrations embeds the SQL migration files so relayd can apply them
// on boot without a separate migrate binary. The .sql files remain usable by
// the golang-migrate CLI (which ignores this .go file).
package migrations

import "embed"

// FS holds all migration SQL files.
//
//go:embed *.sql
var FS embed.FS
