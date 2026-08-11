package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/update"
)

const releaseAPI = "https://api.github.com/repos/OnjLouis/Clipman/releases?per_page=100"

func main() {
	packagePath := flag.String("package", "", "local manifest-v2 update archive")
	installDir := flag.String("install-dir", "", "server installation directory")
	appDir := flag.String("app-dir", "", "legacy name for the server installation directory")
	targetPath := flag.String("target", "", "installed server executable path")
	healthURL := flag.String("health-url", "", "health URL checked after replacement")
	token := flag.String("token", "", "health bearer token")
	check := flag.Bool("check", false, "check for an available update")
	install := flag.Bool("install", false, "install an available update")
	currentVersion := flag.String("current-version", "", "installed server version")
	apiURL := flag.String("release-api-url", releaseAPI, "release discovery API")
	setHost := flag.String("set-host", "", "change configured listen host")
	advertiseHost := flag.String("advertise-host", "", "change configured client host")
	configPath := flag.String("config", "", "server settings path")
	// Accepted for compatibility with deployed Python-era helper scripts. Concrete service coordination remains in the helper.
	_ = flag.String("bin-dir", "", "compatibility option")
	_ = flag.String("service-file", "", "compatibility option")
	_ = flag.String("helper-path", "", "compatibility option")
	_ = flag.String("launcher-path", "", "compatibility option")
	_ = flag.String("init-system", "", "compatibility option")
	_ = flag.Bool("managed-program-only", false, "compatibility option")
	_ = flag.Bool("yes", false, "install without prompting")
	flag.Parse()
	if *setHost != "" {
		changeHost(*configPath, *setHost, *advertiseHost)
		return
	}
	if *installDir == "" {
		*installDir = *appDir
	}
	if *check || (*install && *packagePath == "") {
		version, asset := discover(*apiURL, *currentVersion)
		if *check {
			fmt.Printf("Clipman Server %s is available: %s\n", version, asset.Name)
			return
		}
		temporaryDirectory, err := os.MkdirTemp("", "clipman-server-download-")
		if err != nil {
			fail(err)
		}
		defer os.RemoveAll(temporaryDirectory)
		downloadPath := filepath.Join(temporaryDirectory, asset.Name)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err = update.Download(ctx, asset.URL, asset.Digest, downloadPath); err != nil {
			fail(err)
		}
		*packagePath = downloadPath
	}
	if *packagePath == "" {
		fmt.Fprintln(os.Stderr, "--package is required")
		os.Exit(2)
	}
	if *installDir == "" {
		*installDir = "."
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
	target := *targetPath
	if target == "" {
		target = filepath.Join(*installDir, "clipman-server")
		if runtime.GOOS == "windows" {
			target += ".exe"
		}
	}
	var health update.HealthCheck
	if *healthURL != "" {
		health = update.HTTPHealth(*healthURL, *token)
	}
	if err = update.InstallWithRollback(source, target, health); err != nil {
		fail(err)
	}
	fmt.Printf("Installed Clipman Server %s to %s\n", manifest.Version, target)
}

func discover(apiURL, current string) (string, update.ReleaseAsset) {
	if current == "" {
		fail(fmt.Errorf("--current-version is required"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	releases, err := update.ReadReleases(ctx, apiURL)
	if err != nil {
		fail(err)
	}
	version, asset, err := update.SelectRelease(releases, current, runtime.GOOS, runtime.GOARCH, true)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("Clipman Server is up to date.")
			os.Exit(0)
		}
		fail(err)
	}
	return version, asset
}
func changeHost(path, host, advertised string) {
	if path == "" {
		fail(fmt.Errorf("--config is required with --set-host"))
	}
	loaded, err := config.Load(path)
	if err != nil {
		fail(err)
	}
	loaded.Settings.SetString("Host", host)
	if advertised != "" {
		loaded.Settings.SetString("AdvertiseHost", advertised)
	}
	if err = config.Save(path, loaded.Settings); err != nil {
		fail(err)
	}
	fmt.Printf("Clipman Server listener updated to %s. Restart the service and verify health.\n", host)
}
func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
