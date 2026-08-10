package config

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestSparseLoadDoesNotRewriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := []byte("{\r\n  \"Unknown\": 9007199254740993,\r\n  \"Host\": \"127.0.0.1\"\r\n}")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Created {
		t.Fatal("existing sparse settings reported as created")
	}
	afterData, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterData, original) || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("ordinary sparse load changed settings bytes or mtime")
	}
	if loaded.Settings.String("AuthToken") == "" {
		t.Fatal("defaults were not applied in memory")
	}
}

func TestMarshalMatchesPythonStyle(t *testing.T) {
	data, err := Marshal(map[string]any{"z": "münchen <&>", "a": true})
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": true,\n  \"z\": \"m\\u00fcnchen <&>\"\n}"
	if string(data) != want {
		t.Fatalf("marshal mismatch\n got: %s\nwant: %s", data, want)
	}
}

func TestMissingSettingsArePrivateAndComplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Created || loaded.Settings.String("AuthToken") == "" {
		t.Fatal("missing settings were not created with a token")
	}
	port, err := loaded.Settings.Int("Port")
	if err != nil || port < ServerPortMin || port > ServerPortMax {
		t.Fatalf("unexpected persistent port %d: %v", port, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasSuffix(data, []byte("\n")) {
		t.Fatal("settings must not have a trailing newline")
	}
}

func TestExplicitSavePreservesUnknownExactInteger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"Unknown":9007199254740993,"Host":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Settings.SetString("AdvertiseHost", "münchen.local")
	if err = Save(path, loaded.Settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"Unknown": 9007199254740993`)) {
		t.Fatalf("unknown integer was not preserved exactly: %s", data)
	}
	if !bytes.Contains(data, []byte(`"AdvertiseHost": "m\u00fcnchen.local"`)) {
		t.Fatalf("non-ASCII value was not escaped compatibly: %s", data)
	}
}
