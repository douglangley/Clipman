package compat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPackageServerURLIsRestrictedToLoopback(t *testing.T) {
	for _, value := range []string{
		"https://example.com:443",
		"http://192.168.1.10:8080",
		"file:///tmp/server",
		"http://127.0.0.1:8080/unexpected",
		"http://user@127.0.0.1:8080",
	} {
		if _, err := validatePackageServerURL(value); err == nil {
			t.Fatalf("unsafe package URL %q was accepted", value)
		}
	}
	for _, value := range []string{"http://127.0.0.1:8080", "https://localhost:8443/", "http://[::1]:9000"} {
		if _, err := validatePackageServerURL(value); err != nil {
			t.Fatalf("loopback package URL %q was rejected: %v", value, err)
		}
	}
}

func TestPackageModeRejectsWrapperCoreVersionDriftBeforeMutation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/health" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprint(writer, `{"Status":"ok","Version":"2.6.2"}`)
	}))
	defer server.Close()
	testRoot := filepath.Join(t.TempDir(), ".test-tmp-clipman-server-package-version")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testRoot, packageMarkerFilename), []byte(`{"purpose":"clipman-server-package-compat","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(testRoot, "token.txt")
	if err := os.WriteFile(tokenFile, []byte("dummy-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunPackage(context.Background(), PackageOptions{
		ServerURL: server.URL, TestRoot: testRoot, TokenFile: tokenFile, CLIPath: "unused-cli", ExpectedVersion: "2.6.3", Seed: "test",
	})
	if err == nil || !strings.Contains(err.Error(), "executable reports") {
		t.Fatalf("version drift error = %v", err)
	}
}

func TestPackageModeRequiresMarkedTestRootContainingToken(t *testing.T) {
	parent := t.TempDir()
	testRoot := filepath.Join(parent, ".test-tmp-clipman-server-package-safe")
	if err := os.Mkdir(testRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	tokenFile := filepath.Join(parent, "token.txt")
	if err := os.WriteFile(tokenFile, []byte("dummy-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePackageTestRoot(testRoot, tokenFile); err == nil {
		t.Fatal("unmarked test root was accepted")
	}
	if err := os.WriteFile(filepath.Join(testRoot, packageMarkerFilename), []byte(`{"purpose":"clipman-server-package-compat","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePackageTestRoot(testRoot, tokenFile); err == nil {
		t.Fatal("token outside the marked test root was accepted")
	}
	insideToken := filepath.Join(testRoot, "token.txt")
	if err := os.WriteFile(insideToken, []byte("dummy-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validatePackageTestRoot(testRoot, insideToken); err != nil {
		t.Fatalf("marked test root was rejected: %v", err)
	}
}

func TestPackageTokenRejectsEmbeddedNewline(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "token.txt")
	if err := os.WriteFile(filename, []byte("first\nsecond"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPackageToken(filename); err == nil {
		t.Fatal("token with an embedded newline was accepted")
	}
}
