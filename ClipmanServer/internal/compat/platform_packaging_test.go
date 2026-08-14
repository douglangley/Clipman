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

func TestReleaseLayoutKeepsNativeAndPythonEraNames(t *testing.T) {
	repository := filepath.Clean(filepath.Join("..", "..", ".."))
	cases := []struct {
		path      string
		required  []string
		forbidden []string
	}{
		{
			"ClipmanServerMac/Scripts/package-release-layout.sh",
			[]string{
				"windows-amd64", "macos-universal", "linux-amd64", "linux-arm64", "linux-armv7",
				"/clipman.exe", "/clipmanserver.exe", "/clipman", "/clipmanserver",
				"Clipman Server.exe", "Clipman Server.app", "clipman-cli",
				"run-clipman-server.sh", "install-clipman-server.sh",
				"ClipmanServer-Windows-x64-", "ClipmanServer-macOS-universal-", "ClipmanServer-Linux-amd64-",
			},
			[]string{"clipman_server.py", "python3", "openssl"},
		},
		{
			"ClipmanServerLinux/install-clipman-server.sh",
			[]string{
				"support/clipman-server", "support/clipman-server-updater",
				"clipman-server-$NATIVE_ARCH", "clipman-server-updater-$NATIVE_ARCH",
				`$BIN_DIR/clipman`, `$BIN_DIR/clipman-cli`, `$BIN_DIR/clipman-server`, `$BIN_DIR/clipmanserver`,
			},
			nil,
		},
		{
			"ClipmanServerWindows/Program.cs",
			[]string{"ClipmanServer-Windows-x64-", "ClipmanServer-\" + version + \".zip", "clipmanserver.exe", "Clipman Server.exe"},
			[]string{"clipman_server.py", "FindPythonLauncher"},
		},
		{
			"ClipmanServerWindows/Install-ClipmanServer.ps1",
			[]string{"clipman.exe", "clipman-cli.exe", "clipmanserver.exe", "Clipman Server.exe"},
			[]string{"python.exe", "clipman_server.py", "openssl.exe"},
		},
		{
			"ClipmanServerMac/Scripts/install.sh",
			[]string{"clipman", "clipman-cli", "clipmanserver", "Clipman Server.app"},
			[]string{"python3", "clipman_server.py", "openssl"},
		},
		{
			"ClipmanServerDocker/Dockerfile.release",
			[]string{"docker/binaries/${TARGETARCH}${TARGETVARIANT}/clipman-server", "clipman-server-entrypoint"},
			[]string{"FROM python:", "openssl", "clipman_server.py"},
		},
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
