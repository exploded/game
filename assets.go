// Package game embeds the templates, static files and schema into the binary,
// so a deploy ships one file and the server never reads them from disk.
//
// It lives at the repo root because go:embed can't reach parent directories.
package game

import (
	"embed"
	"io/fs"
)

// all: is needed for templates because a plain directory embed skips files
// starting with "_", which is how partials and fragments are named.
//
//go:embed all:templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

//go:embed sql/schema.sql
var Schema string

// Templates returns the templates directory as the root of an fs.FS.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err) // the path is fixed at compile time
	}
	return sub
}

// Static returns the static directory as the root of an fs.FS.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
