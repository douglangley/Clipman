package platform

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

func DefaultDataDir() (string, error) {
	switch runtime.GOOS {
	case "windows":
		root := firstEnvironment("LOCALAPPDATA", "APPDATA")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(root, "Clipman Server"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "Clipman Server"), nil
	default:
		if root := os.Getenv("XDG_DATA_HOME"); root != "" {
			return filepath.Join(root, "clipman-server"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "clipman-server"), nil
	}
}

func DefaultLogPath() (string, error) {
	switch runtime.GOOS {
	case "windows":
		root, err := DefaultDataDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(root, "logs", "clipman-server.log"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Logs", "Clipman Server", "logs", "clipman-server.log"), nil
	default:
		root := os.Getenv("XDG_STATE_HOME")
		if root == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			root = filepath.Join(home, ".local", "state")
		}
		return filepath.Join(root, "clipman-server", "logs", "clipman-server.log"), nil
	}
}

func DefaultConfigPath(workingDirectory string) (string, error) {
	if workingDirectory == "" {
		return "", errors.New("working directory is required")
	}
	return filepath.Join(workingDirectory, "Settings", "clipman-server-settings.json"), nil
}

func firstEnvironment(names ...string) string {
	for _, name := range names {
		if value := os.Getenv(name); value != "" {
			return value
		}
	}
	return ""
}
