package compat

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHistoricalFixtureInventory(t *testing.T) {
	manifest, err := LoadHistoricalManifest(filepath.Join("..", "..", "compat", "historical-releases.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"2.1.1", "2.4.3", "2.6.3"}
	if got := HistoricalVersions(manifest); !reflect.DeepEqual(got, want) {
		t.Fatalf("historical versions = %v, want %v", got, want)
	}
	if manifest.Releases[0].Capabilities.ManagedProgramOnly {
		t.Fatal("2.1.1 unexpectedly advertises externally managed updates")
	}
	if !manifest.Releases[1].Capabilities.ManagedProgramOnly || manifest.Releases[1].Capabilities.InitSystem {
		t.Fatal("2.4.3 fixture does not identify the managed-update generation")
	}
	if !manifest.Releases[2].Capabilities.Runit || !manifest.Releases[2].Capabilities.InitSystem {
		t.Fatal("2.6.3 fixture does not identify the runit-aware generation")
	}
}

func TestPublishedHistoricalPackages(t *testing.T) {
	directory := os.Getenv("CLIPMAN_HISTORICAL_PACKAGE_DIR")
	if directory == "" {
		t.Skip("set CLIPMAN_HISTORICAL_PACKAGE_DIR to verify published release ZIPs")
	}
	manifest, err := LoadHistoricalManifest(filepath.Join("..", "..", "compat", "historical-releases.json"))
	if err != nil {
		t.Fatal(err)
	}
	probes, err := ValidateHistoricalPackages(manifest, directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(probes) != len(manifest.Releases) {
		t.Fatalf("validated %d packages, want %d", len(probes), len(manifest.Releases))
	}
}

func TestHistoricalManifestRejectsImpossibleRunitCapability(t *testing.T) {
	manifest := HistoricalManifest{
		Version: 1,
		Releases: []HistoricalRelease{{
			Version: "2.6.3", Tag: "server-v2.6.3", PublishedAt: "2026-08-10T20:36:11Z",
			Asset:       HistoricalAsset{Name: "ClipmanServer-2.6.3.zip", Size: 1, SHA256: strings.Repeat("0", 64)},
			PackageRoot: "ClipmanServer",
			Files: []HistoricalFile{
				{Path: "manifest.json", Size: 1, SHA256: strings.Repeat("0", 64)},
				{Path: "clipman_server.py", Size: 1, SHA256: strings.Repeat("0", 64)},
				{Path: "clipman_server_updater.py", Size: 1, SHA256: strings.Repeat("0", 64)},
				{Path: "Linux/install-clipman-server.sh", Size: 1, SHA256: strings.Repeat("0", 64)},
			},
			Capabilities: HistoricalCapabilities{Runit: true},
		}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("impossible runit capability was accepted")
	}
}
