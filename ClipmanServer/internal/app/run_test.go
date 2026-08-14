package app

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCertificatePromptIncludesDetectedAddressesAndHosts(t *testing.T) {
	var output bytes.Buffer
	hosts, addresses, err := promptCertificateNames(
		strings.NewReader("yes\nserver.example, pi.local\n"),
		&output,
		[]string{"192.0.2.8", "2001:db8::8"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hosts, ",") != "server.example,pi.local" {
		t.Fatalf("hosts=%v", hosts)
	}
	if strings.Join(addresses, ",") != "192.0.2.8,2001:db8::8" {
		t.Fatalf("addresses=%v", addresses)
	}
	if !strings.Contains(output.String(), "Detected non-loopback IP addresses:") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestCertificatePromptCanSkipAddressesAndRetriesAnswer(t *testing.T) {
	var output bytes.Buffer
	hosts, addresses, err := promptCertificateNames(strings.NewReader("perhaps\nno\n\n"), &output, []string{"192.0.2.8"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 0 || len(addresses) != 0 {
		t.Fatalf("hosts=%v addresses=%v", hosts, addresses)
	}
	if !strings.Contains(output.String(), "Please answer yes or no.") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestCertificatePromptWithoutDetectedAddressesStillAsksForHosts(t *testing.T) {
	var output bytes.Buffer
	hosts, addresses, err := promptCertificateNames(strings.NewReader("server.example\n"), &output, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(hosts, ",") != "server.example" || len(addresses) != 0 {
		t.Fatalf("hosts=%v addresses=%v", hosts, addresses)
	}
}

func TestCertificatePromptEOFIsCancellation(t *testing.T) {
	_, _, err := promptCertificateNames(strings.NewReader(""), io.Discard, []string{"192.0.2.8"})
	if !errors.Is(err, io.EOF) {
		t.Fatalf("err=%v", err)
	}
}

func TestVersionOutputIsBare(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--version"}, "2.6.2-test", &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	if stdout.String() != "2.6.2-test\n" || stderr.Len() != 0 {
		t.Fatalf("unexpected output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestFirstRunWritesMacWrapperConnectionFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "clipman-server-settings.json")
	var stdout, stderr bytes.Buffer
	// An invalid listener port stops before serving while still exercising the
	// first-run settings and connection-file behavior used by desktop wrappers.
	if code := Run([]string{"--config", path, "--port", "0"}, "test", &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	// Port validation intentionally happens before loading. Use an operation
	// that loads settings and exits without starting a long-running listener.
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--config", path, "--show-token"}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	for _, name := range []string{"clipman-server-connection.txt", "clipman-server-connection.clpconf"} {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("%s was not created: %v", name, err)
		}
	}
}

func TestInvalidSetupBaseURLDoesNotModifySettings(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--config", path, "--show-token"}, "test", &stdout, &stderr); code != 0 {
		t.Fatal(stderr.String())
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--config", path, "--setup-base-url", "http://public.example/path"}, "test", &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("invalid setup URL modified settings")
	}
}

func TestInvalidPortUsesCompatibilityExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	path := filepath.Join(t.TempDir(), "settings.json")
	if code := Run([]string{"--config", path, "--port", "99999"}, "test", &stdout, &stderr); code != 2 {
		t.Fatalf("code %d, stderr %q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "between 1 and 65535") {
		t.Fatalf("unexpected error: %q", stderr.String())
	}
}

func TestSuggestPortPrintsOnlyPersistentPort(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--suggest-port"}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("code %d: %s", code, stderr.String())
	}
	value := strings.TrimSpace(stdout.String())
	if value < "20000" || value > "49151" || strings.Contains(value, " ") {
		t.Fatalf("unexpected port output %q", stdout.String())
	}
}

func TestWrapperSettingQueries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"--config", path, "--host", "127.0.0.9", "--show-host"}, "test", &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "127.0.0.9" {
		t.Fatalf("host code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"--config", path, "--show-database-prune-days"}, "test", &stdout, &stderr); code != 0 || strings.TrimSpace(stdout.String()) != "0" {
		t.Fatalf("days code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
