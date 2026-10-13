// Package web embeds the compiled SvelteKit UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// FS returns the embedded web/build tree with "build" as its root.
func FS() (fs.FS, error) { return fs.Sub(build, "build") }
