package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	MaxDownloadBytes  int64 = 250 << 20
	MaxExtractedBytes int64 = 600 << 20
	MaxEntries              = 2000
)

type Artifact struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	Executable   bool   `json:"executable"`
}
type Manifest struct {
	FormatVersion int        `json:"format_version"`
	Name          string     `json:"name"`
	Version       string     `json:"version"`
	Artifacts     []Artifact `json:"artifacts"`
}

type ReleaseAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
}
type Release struct {
	TagName    string         `json:"tag_name"`
	Draft      bool           `json:"draft"`
	Prerelease bool           `json:"prerelease"`
	Assets     []ReleaseAsset `json:"assets"`
}

func SelectRelease(releases []Release, currentVersion, goos, goarch string, allowSameVersionMigration bool) (string, ReleaseAsset, error) {
	type candidate struct {
		version string
		asset   ReleaseAsset
	}
	items := []candidate{}
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		tag := strings.TrimSpace(release.TagName)
		if !strings.HasPrefix(strings.ToLower(tag), "server-v") {
			continue
		}
		version := tag[len("server-v"):]
		comparison, err := CompareVersions(version, currentVersion)
		if err != nil || comparison < 0 || (comparison == 0 && !allowSameVersionMigration) {
			continue
		}
		wanted := nativeAssetName(version, goos, goarch)
		for _, asset := range release.Assets {
			if strings.EqualFold(asset.Name, wanted) {
				if !strings.HasPrefix(strings.ToLower(asset.URL), "https://") {
					return "", ReleaseAsset{}, errors.New("server update download did not use HTTPS")
				}
				items = append(items, candidate{version, asset})
				break
			}
		}
	}
	if len(items) == 0 {
		return "", ReleaseAsset{}, os.ErrNotExist
	}
	sort.Slice(items, func(i, j int) bool {
		comparison, _ := CompareVersions(items[i].version, items[j].version)
		return comparison > 0
	})
	return items[0].version, items[0].asset, nil
}
func nativeAssetName(version, goos, goarch string) string {
	switch goos {
	case "windows":
		return fmt.Sprintf("ClipmanServer-Windows-x64-%s.zip", version)
	case "darwin":
		return fmt.Sprintf("ClipmanServer-macOS-universal-%s.zip", version)
	default:
		arch := goarch
		if arch == "arm" {
			arch = "armv7"
		}
		return fmt.Sprintf("ClipmanServer-Linux-%s-%s.tar.gz", arch, version)
	}
}

func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	if m.FormatVersion != 2 {
		return m, fmt.Errorf("unsupported package manifest version %d", m.FormatVersion)
	}
	if m.Name != "Clipman Server" || m.Version == "" {
		return m, errors.New("package manifest identity is invalid")
	}
	if _, err := versionTuple(m.Version); err != nil {
		return m, err
	}
	return m, nil
}
func (m Manifest) Select(goos, goarch string) (Artifact, error) {
	for _, a := range m.Artifacts {
		if a.OS == goos && a.Architecture == goarch {
			if !safeRelative(a.Path) {
				return Artifact{}, errors.New("manifest artifact path is unsafe")
			}
			if len(a.SHA256) != 64 {
				return Artifact{}, errors.New("manifest artifact digest is invalid")
			}
			return a, nil
		}
	}
	return Artifact{}, fmt.Errorf("package has no artifact for %s/%s", goos, goarch)
}
func ExtractPackage(zipPath, destination string) (Manifest, string, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return Manifest{}, "", err
	}
	defer reader.Close()
	if len(reader.File) > MaxEntries {
		return Manifest{}, "", errors.New("update package contains too many files")
	}
	var total uint64
	for _, entry := range reader.File {
		total += entry.UncompressedSize64
		if total > uint64(MaxExtractedBytes) {
			return Manifest{}, "", errors.New("extracted update would be unexpectedly large")
		}
		if !safeRelative(entry.Name) || entry.Mode()&os.ModeSymlink != 0 {
			return Manifest{}, "", fmt.Errorf("unsafe archive entry: %s", entry.Name)
		}
	}
	if err = os.MkdirAll(destination, 0o700); err != nil {
		return Manifest{}, "", err
	}
	for _, entry := range reader.File {
		target := filepath.Join(destination, filepath.FromSlash(entry.Name))
		if entry.FileInfo().IsDir() {
			if err = os.MkdirAll(target, 0o755); err != nil {
				return Manifest{}, "", err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return Manifest{}, "", err
		}
		source, openErr := entry.Open()
		if openErr != nil {
			return Manifest{}, "", openErr
		}
		mode := os.FileMode(0o644)
		if entry.Mode()&0o111 != 0 {
			mode = 0o755
		}
		out, createErr := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if createErr != nil {
			source.Close()
			return Manifest{}, "", createErr
		}
		_, copyErr := io.Copy(out, source)
		closeErr := out.Close()
		source.Close()
		if copyErr != nil {
			return Manifest{}, "", copyErr
		}
		if closeErr != nil {
			return Manifest{}, "", closeErr
		}
	}
	manifests := []string{}
	_ = filepath.WalkDir(destination, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr == nil && !d.IsDir() && d.Name() == "manifest-v2.json" {
			manifests = append(manifests, path)
		}
		return walkErr
	})
	if len(manifests) != 1 {
		return Manifest{}, "", errors.New("update package did not contain exactly one manifest-v2.json")
	}
	data, err := os.ReadFile(manifests[0])
	if err != nil {
		return Manifest{}, "", err
	}
	manifest, err := ParseManifest(data)
	return manifest, filepath.Dir(manifests[0]), err
}
func VerifyArtifact(root string, a Artifact) (string, error) {
	path := filepath.Join(root, filepath.FromSlash(a.Path))
	data, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer data.Close()
	digest := sha256.New()
	if _, err = io.Copy(digest, data); err != nil {
		return "", err
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(strings.ToLower(a.SHA256)), []byte(actual)) != 1 {
		return "", errors.New("artifact failed its SHA-256 check")
	}
	return path, nil
}

type HealthCheck func(context.Context) error

type ServiceController interface {
	Stop(context.Context) error
	Start(context.Context) error
	Diagnostics(context.Context) string
}

func CoordinatedInstall(source, target string, service ServiceController, check HealthCheck) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := service.Stop(ctx); err != nil {
		return fmt.Errorf("stop server service: %w", err)
	}
	started := false
	wrapped := func(checkContext context.Context) error {
		if err := service.Start(checkContext); err != nil {
			return err
		}
		started = true
		if check != nil {
			return check(checkContext)
		}
		return nil
	}
	err := InstallWithRollback(source, target, wrapped)
	if err == nil {
		return nil
	}
	diagnostics := service.Diagnostics(ctx)
	if started {
		_ = service.Stop(ctx)
	}
	restartErr := service.Start(ctx)
	if restartErr != nil {
		return fmt.Errorf("%w; diagnostics: %s; restored service failed to start: %v", err, diagnostics, restartErr)
	}
	return fmt.Errorf("%w; diagnostics: %s", err, diagnostics)
}

func InstallWithRollback(source, target string, check HealthCheck) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o755)
	if stat, statErr := os.Stat(target); statErr == nil {
		mode = stat.Mode().Perm()
	}
	directory := filepath.Dir(target)
	if err = os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	staged, err := os.CreateTemp(directory, ".clipman-update-*.tmp")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if err = staged.Chmod(mode); err == nil {
		_, err = staged.Write(data)
	}
	if closeErr := staged.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	backup := target + ".rollback"
	hadOld := false
	if _, statErr := os.Stat(target); statErr == nil {
		_ = os.Remove(backup)
		if err = os.Rename(target, backup); err != nil {
			return err
		}
		hadOld = true
	}
	if err = os.Rename(stagedPath, target); err != nil {
		if hadOld {
			_ = os.Rename(backup, target)
		}
		return err
	}
	if check != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err = check(ctx); err != nil {
			_ = os.Remove(target)
			if hadOld {
				_ = os.Rename(backup, target)
			}
			return fmt.Errorf("updated server failed health check; rollback completed: %w", err)
		}
	}
	if hadOld {
		_ = os.Remove(backup)
	}
	return nil
}
func HTTPHealth(url, token string) HealthCheck {
	return func(ctx context.Context) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("health returned %s", response.Status)
		}
		return nil
	}
}
func Download(ctx context.Context, url, expectedDigest, destination string) error {
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return errors.New("update download must use HTTPS")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.Request.URL.Scheme != "https" {
		return errors.New("update download redirected outside HTTPS")
	}
	if response.StatusCode != 200 {
		return fmt.Errorf("download returned %s", response.Status)
	}
	limited := io.LimitReader(response.Body, MaxDownloadBytes+1)
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	digest := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, digest), limited)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > MaxDownloadBytes {
		return errors.New("update package was unexpectedly large")
	}
	actual := hex.EncodeToString(digest.Sum(nil))
	expected := strings.TrimPrefix(strings.ToLower(expectedDigest), "sha256:")
	if len(expected) != 64 || subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) != 1 {
		return errors.New("downloaded update failed its SHA-256 check")
	}
	return nil
}
func CurrentArtifact(m Manifest) (Artifact, error) { return m.Select(runtime.GOOS, runtime.GOARCH) }
func versionTuple(value string) ([]int, error) {
	parts := strings.Split(strings.TrimLeft(strings.TrimSpace(value), "vV"), ".")
	if len(parts) < 2 || len(parts) > 4 {
		return nil, fmt.Errorf("invalid stable version: %s", value)
	}
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid stable version: %s", value)
		}
		out[i] = n
	}
	return out, nil
}
func CompareVersions(a, b string) (int, error) {
	left, err := versionTuple(a)
	if err != nil {
		return 0, err
	}
	right, err := versionTuple(b)
	if err != nil {
		return 0, err
	}
	size := max(len(left), len(right))
	left = append(left, make([]int, size-len(left))...)
	right = append(right, make([]int, size-len(right))...)
	for i := range size {
		if left[i] < right[i] {
			return -1, nil
		}
		if left[i] > right[i] {
			return 1, nil
		}
	}
	return 0, nil
}
func safeRelative(value string) bool {
	clean := strings.ReplaceAll(value, "\\", "/")
	if clean == "" || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return false
	}
	for _, part := range strings.Split(clean, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return filepath.Clean(filepath.FromSlash(clean)) != filepath.Clean(".")
}
