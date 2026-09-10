package main

import (
	"embed"
	"io/fs"
)

// The two SPA bundles are embedded at build time. `make desktop-frontend`
// copies the vite dists into web/admin and web/canvas; without them the app
// still boots and serves the placeholder page (see webmux.go).
//
//go:embed all:web/admin
var adminDist embed.FS

//go:embed all:web/canvas
var canvasDist embed.FS

func adminSPA() fs.FS {
	sub, err := fs.Sub(adminDist, "web/admin")
	if err != nil {
		panic(err) // static embed layout, cannot fail at runtime
	}
	return sub
}

func canvasSPA() fs.FS {
	sub, err := fs.Sub(canvasDist, "web/canvas")
	if err != nil {
		panic(err)
	}
	return sub
}
