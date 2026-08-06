// Package migrations embeds the numbered SQL files so cmd/migrate ships as a
// single binary with no runtime dependency on the source tree.
//
// The embed directive has to live in this directory: //go:embed cannot
// reference paths outside its own package.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
