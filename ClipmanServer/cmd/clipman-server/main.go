package main

import (
	"os"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/app"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo"
)

// version is populated by -ldflags for release builds. Development builds use
// the explicit buildinfo fallback so the source of a non-release version is
// always visible in --version output.
var version string

func main() {
	value := version
	if value == "" {
		value = buildinfo.Version
	}
	os.Exit(app.Run(os.Args[1:], value, os.Stdout, os.Stderr))
}
