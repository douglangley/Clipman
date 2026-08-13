package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/compat"
)

type report struct {
	Mode               string                          `json:"mode"`
	Coverage           compat.Counts                   `json:"coverage"`
	HistoricalFixtures int                             `json:"historical_fixtures"`
	HistoricalPackages []compat.HistoricalPackageProbe `json:"historical_packages,omitempty"`
	Probes             []compat.Probe                  `json:"probes"`
	Package            *compat.PackageResult           `json:"package,omitempty"`
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	set := flag.NewFlagSet("clipman-server-compat", flag.ContinueOnError)
	set.SetOutput(os.Stderr)
	mode := set.String("mode", "reference", "reference, differential, go-only, or package")
	coveragePath := set.String("coverage", "compat/coverage.json", "coverage manifest")
	historicalPath := set.String("historical-releases", "compat/historical-releases.json", "historical release fixture manifest")
	historicalPackages := set.String("historical-packages", "", "directory containing historical release ZIPs to verify")
	pythonServer := set.String("python-server", "", "Python server script")
	goServer := set.String("go-server", "", "Go server executable")
	clipmanCLI := set.String("clipman-cli", "", "Clipman CLI executable")
	serverURL := set.String("server-url", "", "installed package server URL (package mode)")
	testRoot := set.String("test-root", "", "marked isolated root used by package mode")
	tokenFile := set.String("token-file", "", "installed package token file (package mode)")
	caCertificate := set.String("ca-cert", "", "private CA certificate for the installed package")
	seed := set.String("seed", "20260813", "deterministic package corpus seed")
	if err := set.Parse(args); err != nil {
		return 2
	}
	manifest, err := compat.LoadManifest(*coveragePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", err)
		return 1
	}
	historical, err := compat.LoadHistoricalManifest(*historicalPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", err)
		return 1
	}
	var specifications []struct {
		name, path string
		python     bool
	}
	switch *mode {
	case "reference":
		specifications = append(specifications,
			struct {
				name, path string
				python     bool
			}{"python-server", *pythonServer, true},
			struct {
				name, path string
				python     bool
			}{"clipman-cli", *clipmanCLI, false},
		)
	case "differential":
		specifications = append(specifications,
			struct {
				name, path string
				python     bool
			}{"python-server", *pythonServer, true},
			struct {
				name, path string
				python     bool
			}{"go-server", *goServer, false},
			struct {
				name, path string
				python     bool
			}{"clipman-cli", *clipmanCLI, false},
		)
	case "go-only", "package":
		specifications = append(specifications,
			struct {
				name, path string
				python     bool
			}{"go-server", *goServer, false},
			struct {
				name, path string
				python     bool
			}{"clipman-cli", *clipmanCLI, false},
		)
	default:
		fmt.Fprintf(os.Stderr, "clipman-server-compat: unsupported mode %q\n", *mode)
		return 2
	}
	result := report{Mode: *mode, Coverage: manifest.Counts(), HistoricalFixtures: len(historical.Releases)}
	if *historicalPackages != "" {
		result.HistoricalPackages, err = compat.ValidateHistoricalPackages(historical, *historicalPackages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", err)
			return 1
		}
	}
	for _, specification := range specifications {
		probe, probeErr := compat.ProbeExecutable(context.Background(), specification.name, specification.path, specification.python)
		if probeErr != nil {
			fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", probeErr)
			return 1
		}
		result.Probes = append(result.Probes, probe)
	}
	if *mode == "package" {
		expectedVersion := ""
		for _, probe := range result.Probes {
			if probe.Program == "go-server" {
				expectedVersion = probe.Version
				break
			}
		}
		packageResult, packageErr := compat.RunPackage(context.Background(), compat.PackageOptions{
			ServerURL:       *serverURL,
			TokenFile:       *tokenFile,
			CACertificate:   *caCertificate,
			CLIPath:         *clipmanCLI,
			ExpectedVersion: expectedVersion,
			Seed:            *seed,
			TestRoot:        *testRoot,
		})
		if packageErr != nil {
			fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", packageErr)
			return 1
		}
		result.Package = &packageResult
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(result); err != nil {
		fmt.Fprintf(os.Stderr, "clipman-server-compat: %v\n", err)
		return 1
	}
	return 0
}
