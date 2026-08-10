package certificates

import (
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
