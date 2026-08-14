package certificates

import (
	"net/netip"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateAndRenewWithSameCA(t *testing.T) {
	config := filepath.Join(t.TempDir(), "settings.json")
	now := time.Now().UTC().Truncate(time.Second)
	first, err := generate(config, []string{"server.test"}, nil, false, 1024, 1024, now)
	if err != nil {
		t.Fatal(err)
	}
	second, err := generate(config, []string{"server.test"}, nil, false, 1024, 1024, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint {
		t.Fatal("renewal replaced CA")
	}
	fingerprint, _, err := InspectCA(second.Authority, second.Certificate, "server.test")
	if err != nil || fingerprint != first.Fingerprint {
		t.Fatalf("inspect fingerprint=%s err=%v", fingerprint, err)
	}
}

func TestCertificateDNSNamesUseStrictLabels(t *testing.T) {
	valid := []string{"server.test", "pi-1.local", "localhost"}
	for _, hostname := range valid {
		if _, _, err := normalizeNames([]string{hostname}, nil); err != nil {
			t.Errorf("valid hostname %q rejected: %v", hostname, err)
		}
	}
	invalid := []string{"bad..example", "-bad.example", "bad-.example", "bad_example", "bad example"}
	for _, hostname := range invalid {
		if _, _, err := normalizeNames([]string{hostname}, nil); err == nil {
			t.Errorf("invalid hostname %q accepted", hostname)
		}
	}
}

func TestUsableCertificateIP(t *testing.T) {
	for _, value := range []string{"192.0.2.8", "2001:db8::8", "fe80::1234"} {
		if !usableCertificateIP(netip.MustParseAddr(value)) {
			t.Errorf("usable address %s rejected", value)
		}
	}
	for _, value := range []string{"127.0.0.1", "::1", "0.0.0.0", "ff02::1"} {
		if usableCertificateIP(netip.MustParseAddr(value)) {
			t.Errorf("unusable address %s accepted", value)
		}
	}
}
