package compat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalDesktopAndContainerLaunchPathsUseNativeCore(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", ".."))
	cases := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{"ClipmanServerWindows/Program.cs", []string{"clipman-server.exe", "ExtractBundledServerCore"}, []string{"FindPythonLauncher", "clipman_server.py"}},
		{"ClipmanServerMac/Sources/ClipmanServer/main.swift", []string{"appendingPathComponent(\"clipman-server\")"}, []string{"findPython", "clipman_server.py"}},
		{"ClipmanServerDocker/Dockerfile", []string{"FROM golang:", "/usr/local/bin/clipman-server"}, []string{"FROM python:", "openssl"}},
		{"ClipmanServerDocker/docker-entrypoint.sh", []string{"SERVER_BINARY", "exec \"$@\""}, []string{"python3", "SERVER_SCRIPT"}},
		{"ClipmanServerLinux/install-clipman-server.sh", []string{"clipman-server-$NATIVE_ARCH", "clipman-server-updater"}, []string{"python3", "clipman_server.py"}},
		{"ClipmanServerLinux/install-clipman-server-system-helper.sh", []string{"$APP_DIR/clipman-server", "$APP_DIR/clipman-server-updater"}, []string{"python3", "clipman_server.py"}},
	}
	for _, item := range cases {
		data, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(item.path)))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, value := range item.required {
			if !strings.Contains(text, value) {
				t.Errorf("%s is missing %q", item.path, value)
			}
		}
		for _, value := range item.forbidden {
			if strings.Contains(text, value) {
				t.Errorf("%s still contains %q", item.path, value)
			}
		}
	}
}
