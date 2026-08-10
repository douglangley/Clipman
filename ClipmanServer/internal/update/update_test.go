package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManifestSelectsArchitecture(t *testing.T) {
	sum := sha256.Sum256([]byte("binary"))
	m, err := ParseManifest([]byte(`{"format_version":2,"name":"Clipman Server","version":"3.0.0","artifacts":[{"os":"linux","architecture":"amd64","path":"bin/server","sha256":"` + hex.EncodeToString(sum[:]) + `","executable":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	a, err := m.Select("linux", "amd64")
	if err != nil || a.Path != "bin/server" {
		t.Fatalf("artifact=%+v err=%v", a, err)
	}
}
func TestUnsafeArchiveRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.zip")
	file, _ := os.Create(path)
	writer := zip.NewWriter(file)
	entry, _ := writer.Create("../escape")
	_, _ = entry.Write([]byte("bad"))
	_ = writer.Close()
	_ = file.Close()
	if _, _, err := ExtractPackage(path, t.TempDir()); err == nil {
		t.Fatal("unsafe archive accepted")
	}
}

func TestPackageExtractionAndArtifactVerification(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package.zip")
	file, _ := os.Create(path)
	writer := zip.NewWriter(file)
	binary := []byte("server-binary")
	sum := sha256.Sum256(binary)
	manifest := []byte(`{"format_version":2,"name":"Clipman Server","version":"3.0.0","artifacts":[{"os":"linux","architecture":"amd64","path":"bin/server","sha256":"` + hex.EncodeToString(sum[:]) + `","executable":true}]}`)
	for name, data := range map[string][]byte{"package/manifest-v2.json": manifest, "package/bin/server": binary} {
		entry, _ := writer.Create(name)
		_, _ = entry.Write(data)
	}
	_ = writer.Close()
	_ = file.Close()
	m, root, err := ExtractPackage(path, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := m.Select("linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyArtifact(root, artifact)
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(verified); string(data) != string(binary) {
		t.Fatal("verified artifact mismatch")
	}
}
func TestFailedHealthRollsBack(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "new")
	target := filepath.Join(dir, "server")
	_ = os.WriteFile(source, []byte("new"), 0o755)
	_ = os.WriteFile(target, []byte("old"), 0o755)
	err := InstallWithRollback(source, target, func(context.Context) error { return os.ErrInvalid })
	if err == nil {
		t.Fatal("health failure accepted")
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old" {
		t.Fatalf("rollback data=%q", data)
	}
}

type fakeService struct{ starts, stops int }

func (s *fakeService) Stop(context.Context) error         { s.stops++; return nil }
func (s *fakeService) Start(context.Context) error        { s.starts++; return nil }
func (s *fakeService) Diagnostics(context.Context) string { return "test diagnostics" }
func TestCoordinatedInstallRestartsRestoredService(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "new")
	target := filepath.Join(dir, "server")
	_ = os.WriteFile(source, []byte("new"), 0o755)
	_ = os.WriteFile(target, []byte("old"), 0o755)
	service := &fakeService{}
	err := CoordinatedInstall(source, target, service, func(context.Context) error { return errors.New("unhealthy") })
	if err == nil {
		t.Fatal("failure accepted")
	}
	if service.starts != 2 || service.stops != 2 {
		t.Fatalf("starts=%d stops=%d", service.starts, service.stops)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "old" {
		t.Fatalf("restored=%q", data)
	}
}
func TestVersionComparison(t *testing.T) {
	got, err := CompareVersions("2.10.0", "2.9.9")
	if err != nil || got != 1 {
		t.Fatalf("got=%d err=%v", got, err)
	}
}

func TestReleaseSelectionSupportsSameVersionPythonMigration(t *testing.T) {
	releases := []Release{{TagName: "server-v3.0.0", Assets: []ReleaseAsset{{Name: "ClipmanServer-Linux-amd64-3.0.0.tar.gz", URL: "https://example.test/native.tar.gz", Digest: "sha256:" + strings.Repeat("a", 64)}}}}
	version, asset, err := SelectRelease(releases, "3.0.0", "linux", "amd64", true)
	if err != nil || version != "3.0.0" || asset.URL == "" {
		t.Fatalf("version=%s asset=%+v err=%v", version, asset, err)
	}
	if _, _, err = SelectRelease(releases, "3.0.0", "linux", "amd64", false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no update, got %v", err)
	}
}
