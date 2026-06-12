package webui

import (
	"embed"
	"io/fs"
)

// Include all frontend build artifacts, including files beginning with '_' or '.'.
//
//go:embed all:dist
var embeddedDist embed.FS

func EmbeddedDist() fs.FS {
	dist, err := fs.Sub(embeddedDist, "dist")
	if err != nil {
		return embeddedDist
	}
	return dist
}
