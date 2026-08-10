package compat

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type Probe struct {
	Program string   `json:"program"`
	Version string   `json:"version"`
	Command []string `json:"-"`
}

func ProbeExecutable(ctx context.Context, name, path string, python bool) (Probe, error) {
	if strings.TrimSpace(path) == "" {
		return Probe{}, fmt.Errorf("%s path is required", name)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Probe{}, err
	}
	command := []string{absolute, "--version"}
	if python {
		command = []string{"python", absolute, "--version"}
	}
	probeContext, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	completed := exec.CommandContext(probeContext, command[0], command[1:]...)
	output, err := completed.Output()
	if errors.Is(probeContext.Err(), context.DeadlineExceeded) {
		return Probe{}, fmt.Errorf("%s version probe timed out", name)
	}
	if err != nil {
		return Probe{}, fmt.Errorf("%s version probe failed: %w", name, err)
	}
	version := strings.TrimSpace(string(output))
	if version == "" || strings.ContainsAny(version, "\r\n") {
		return Probe{}, fmt.Errorf("%s returned an invalid version line", name)
	}
	return Probe{Program: name, Version: version, Command: command}, nil
}
