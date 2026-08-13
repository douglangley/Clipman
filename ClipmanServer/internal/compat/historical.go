package compat

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const maxHistoricalFixtureFileBytes = 2 << 20

var stableHistoricalVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

type HistoricalManifest struct {
	Version  int                 `json:"version"`
	Releases []HistoricalRelease `json:"releases"`
}

type HistoricalRelease struct {
	Version      string                 `json:"version"`
	Tag          string                 `json:"tag"`
	PublishedAt  string                 `json:"published_at"`
	Asset        HistoricalAsset        `json:"asset"`
	PackageRoot  string                 `json:"package_root"`
	Files        []HistoricalFile       `json:"files"`
	Capabilities HistoricalCapabilities `json:"capabilities"`
}

type HistoricalAsset struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type HistoricalFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type HistoricalCapabilities struct {
	SetHost            bool `json:"set_host"`
	ManagedProgramOnly bool `json:"managed_program_only"`
	InitSystem         bool `json:"init_system"`
	Runit              bool `json:"runit"`
	SystemHelper       bool `json:"system_helper"`
}

type HistoricalPackageProbe struct {
	Version string `json:"version"`
	Asset   string `json:"asset"`
	SHA256  string `json:"sha256"`
}

func LoadHistoricalManifest(filename string) (HistoricalManifest, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return HistoricalManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest HistoricalManifest
	if err = decoder.Decode(&manifest); err != nil {
		return HistoricalManifest{}, err
	}
	if err = requireJSONEOF(decoder); err != nil {
		return HistoricalManifest{}, err
	}
	if err = manifest.Validate(); err != nil {
		return HistoricalManifest{}, err
	}
	return manifest, nil
}

func (m HistoricalManifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("unsupported historical fixture version %d", m.Version)
	}
	if len(m.Releases) == 0 {
		return errors.New("historical fixture has no releases")
	}
	seen := make(map[string]bool, len(m.Releases))
	previous := ""
	for _, release := range m.Releases {
		if !stableHistoricalVersion.MatchString(release.Version) {
			return fmt.Errorf("historical release has invalid version %q", release.Version)
		}
		if seen[release.Version] {
			return fmt.Errorf("duplicate historical release %q", release.Version)
		}
		seen[release.Version] = true
		if previous != "" && compareStableVersions(previous, release.Version) >= 0 {
			return errors.New("historical releases must be ordered by increasing version")
		}
		previous = release.Version
		if release.Tag != "server-v"+release.Version {
			return fmt.Errorf("historical release %s has mismatched tag %q", release.Version, release.Tag)
		}
		if _, err := time.Parse(time.RFC3339, release.PublishedAt); err != nil {
			return fmt.Errorf("historical release %s has invalid publication time: %w", release.Version, err)
		}
		if release.Asset.Name != "ClipmanServer-"+release.Version+".zip" || release.Asset.Size <= 0 || !validSHA256(release.Asset.SHA256) {
			return fmt.Errorf("historical release %s has invalid asset metadata", release.Version)
		}
		if !safeFixturePath(release.PackageRoot) || strings.Contains(release.PackageRoot, "/") {
			return fmt.Errorf("historical release %s has unsafe package root", release.Version)
		}
		files := make(map[string]bool, len(release.Files))
		for _, file := range release.Files {
			if !safeFixturePath(file.Path) || file.Size <= 0 || file.Size > maxHistoricalFixtureFileBytes || !validSHA256(file.SHA256) {
				return fmt.Errorf("historical release %s has invalid file fixture %q", release.Version, file.Path)
			}
			key := strings.ToLower(file.Path)
			if files[key] {
				return fmt.Errorf("historical release %s repeats file fixture %q", release.Version, file.Path)
			}
			files[key] = true
		}
		for _, required := range []string{"manifest.json", "clipman_server.py", "clipman_server_updater.py", "Linux/install-clipman-server.sh"} {
			if !files[strings.ToLower(required)] {
				return fmt.Errorf("historical release %s is missing fixture %q", release.Version, required)
			}
		}
		if release.Capabilities.Runit && !release.Capabilities.InitSystem {
			return fmt.Errorf("historical release %s enables runit without init-system selection", release.Version)
		}
	}
	return nil
}

func ValidateHistoricalPackages(manifest HistoricalManifest, directory string) ([]HistoricalPackageProbe, error) {
	probes := make([]HistoricalPackageProbe, 0, len(manifest.Releases))
	for _, release := range manifest.Releases {
		archivePath := filepath.Join(directory, release.Asset.Name)
		if err := validateHistoricalPackage(release, archivePath); err != nil {
			return nil, fmt.Errorf("historical release %s: %w", release.Version, err)
		}
		probes = append(probes, HistoricalPackageProbe{Version: release.Version, Asset: release.Asset.Name, SHA256: release.Asset.SHA256})
	}
	return probes, nil
}

func validateHistoricalPackage(release HistoricalRelease, archivePath string) error {
	stat, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	if stat.Size() != release.Asset.Size {
		return fmt.Errorf("asset size is %d, expected %d", stat.Size(), release.Asset.Size)
	}
	actual, err := fileSHA256(archivePath)
	if err != nil {
		return err
	}
	if actual != release.Asset.SHA256 {
		return fmt.Errorf("asset SHA-256 is %s, expected %s", actual, release.Asset.SHA256)
	}
	archive, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer archive.Close()
	entries := make(map[string]*zip.File, len(archive.File))
	caseNames := make(map[string]string, len(archive.File))
	for _, entry := range archive.File {
		name, safe := normalizedArchivePath(entry.Name)
		if !safe {
			return fmt.Errorf("archive contains unsafe path %q", entry.Name)
		}
		key := strings.ToLower(name)
		if prior, exists := caseNames[key]; exists && prior != name {
			return fmt.Errorf("archive paths collide by case: %q and %q", prior, name)
		}
		caseNames[key] = name
		if !entry.FileInfo().IsDir() {
			if _, exists := entries[name]; exists {
				return fmt.Errorf("archive repeats path %q", name)
			}
			entries[name] = entry
		}
	}
	contents := make(map[string][]byte, len(release.Files))
	for _, fixture := range release.Files {
		name := path.Join(release.PackageRoot, fixture.Path)
		entry := entries[name]
		if entry == nil {
			return fmt.Errorf("archive is missing %q", name)
		}
		if int64(entry.UncompressedSize64) != fixture.Size {
			return fmt.Errorf("%s has size %d, expected %d", fixture.Path, entry.UncompressedSize64, fixture.Size)
		}
		data, readErr := readHistoricalEntry(entry, fixture.Size)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", fixture.Path, readErr)
		}
		if dataSHA256(data) != fixture.SHA256 {
			return fmt.Errorf("%s failed its fixture SHA-256 check", fixture.Path)
		}
		contents[fixture.Path] = data
	}
	if err = validateLegacyManifest(contents["manifest.json"], release.Version); err != nil {
		return err
	}
	if err = validateHistoricalCapabilities(contents["clipman_server_updater.py"], release.Capabilities); err != nil {
		return err
	}
	helperPath := path.Join(release.PackageRoot, "Linux/install-clipman-server-system-helper.sh")
	_, hasSystemHelper := entries[helperPath]
	if hasSystemHelper != release.Capabilities.SystemHelper {
		return fmt.Errorf("system-helper presence is %t, expected %t", hasSystemHelper, release.Capabilities.SystemHelper)
	}
	return nil
}

func validateLegacyManifest(data []byte, version string) error {
	var manifest struct {
		Name          string `json:"Name"`
		Version       string `json:"Version"`
		ServerProgram string `json:"ServerProgram"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse historical manifest: %w", err)
	}
	if manifest.Name != "Clipman Server" || manifest.Version != version || manifest.ServerProgram != "clipman_server.py" {
		return errors.New("historical package manifest does not describe the expected Python server")
	}
	return nil
}

func validateHistoricalCapabilities(updater []byte, expected HistoricalCapabilities) error {
	checks := []struct {
		name  string
		text  string
		value bool
	}{
		{"set-host", "--set-host", expected.SetHost},
		{"managed-program-only", "--managed-program-only", expected.ManagedProgramOnly},
		{"init-system", "--init-system", expected.InitSystem},
		{"runit", "runit", expected.Runit},
	}
	for _, check := range checks {
		actual := bytes.Contains(updater, []byte(check.text))
		if actual != check.value {
			return fmt.Errorf("updater capability %s is %t, expected %t", check.name, actual, check.value)
		}
	}
	return nil
}

func readHistoricalEntry(entry *zip.File, size int64) ([]byte, error) {
	reader, err := entry.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != size {
		return nil, errors.New("archive entry length changed while reading")
	}
	return data, nil
}

func normalizedArchivePath(value string) (string, bool) {
	if value == "" || strings.Contains(value, "\\") || strings.HasPrefix(value, "/") {
		return "", false
	}
	trimmed := strings.TrimSuffix(value, "/")
	clean := path.Clean(trimmed)
	if clean == "." || clean != trimmed || strings.Contains(clean, ":") {
		return "", false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return clean, true
}

func safeFixturePath(value string) bool {
	_, ok := normalizedArchivePath(value)
	return ok && !strings.HasSuffix(value, "/")
}

func fileSHA256(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func dataSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func compareStableVersions(left, right string) int {
	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	for index := range leftParts {
		leftValue, _ := strconv.Atoi(leftParts[index])
		rightValue, _ := strconv.Atoi(rightParts[index])
		if leftValue < rightValue {
			return -1
		}
		if leftValue > rightValue {
			return 1
		}
	}
	return 0
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains more than one value")
		}
		return err
	}
	return nil
}

func HistoricalVersions(manifest HistoricalManifest) []string {
	versions := make([]string, 0, len(manifest.Releases))
	for _, release := range manifest.Releases {
		versions = append(versions, release.Version)
	}
	sort.Slice(versions, func(left, right int) bool {
		return compareStableVersions(versions[left], versions[right]) < 0
	})
	return versions
}
