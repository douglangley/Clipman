package compat

import "testing"

func TestManifestRejectsDuplicateContracts(t *testing.T) {
	manifest := Manifest{Version: 1, Contracts: []Contract{
		{ID: "same", Surface: "protocol", Owner: "raw", Platforms: []string{"windows"}, Status: "pending"},
		{ID: "same", Surface: "protocol", Owner: "raw", Platforms: []string{"linux"}, Status: "pending"},
	}}
	if err := manifest.Validate(); err == nil {
		t.Fatal("duplicate contract was accepted")
	}
}

func TestManifestCounts(t *testing.T) {
	manifest := Manifest{Contracts: []Contract{{Status: "implemented"}, {Status: "in-progress"}, {Status: "pending"}, {Status: "pending"}}}
	counts := manifest.Counts()
	if counts.Implemented != 1 || counts.InProgress != 1 || counts.Pending != 2 {
		t.Fatalf("unexpected counts: %+v", counts)
	}
}
