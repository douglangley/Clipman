package main

import (
	"flag"
	"fmt"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/update"
	"os"
	"path/filepath"
)

func main() {
	packagePath := flag.String("package", "", "local manifest-v2 update zip")
	installDir := flag.String("install-dir", ".", "server installation directory")
	healthURL := flag.String("health-url", "", "health URL checked after replacement")
	token := flag.String("token", "", "health bearer token")
	flag.Parse()
	if *packagePath == "" {
		fmt.Fprintln(os.Stderr, "--package is required")
		os.Exit(2)
	}
	staging, err := os.MkdirTemp("", "clipman-server-update-")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(staging)
	manifest, root, err := update.ExtractPackage(*packagePath, staging)
	if err != nil {
		fail(err)
	}
	artifact, err := update.CurrentArtifact(manifest)
	if err != nil {
		fail(err)
	}
	source, err := update.VerifyArtifact(root, artifact)
	if err != nil {
		fail(err)
	}
	target := filepath.Join(*installDir, filepath.Base(artifact.Path))
	var check update.HealthCheck
	if *healthURL != "" {
		check = update.HTTPHealth(*healthURL, *token)
	}
	if err = update.InstallWithRollback(source, target, check); err != nil {
		fail(err)
	}
	fmt.Printf("Installed Clipman Server %s to %s\n", manifest.Version, target)
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
