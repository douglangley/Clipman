package compat

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxPackageTokenBytes  = 8 << 10
	maxPackageHealthBytes = 1 << 20
	packageMarkerFilename = "clipman-package-test.json"
)

type PackageOptions struct {
	ServerURL       string
	TokenFile       string
	CACertificate   string
	CLIPath         string
	ExpectedVersion string
	Seed            string
	TestRoot        string
}

type PackageResult struct {
	ServerVersion string   `json:"server_version"`
	Seed          string   `json:"seed"`
	PayloadSHA256 string   `json:"payload_sha256"`
	Operations    []string `json:"operations"`
}

type packageHealth struct {
	Status  string `json:"Status"`
	Version string `json:"Version"`
}

type packageEntry struct {
	Name string `json:"Name"`
	Text string `json:"Text"`
}

func RunPackage(ctx context.Context, options PackageOptions) (PackageResult, error) {
	testRoot, tokenFile, err := validatePackageTestRoot(options.TestRoot, options.TokenFile)
	if err != nil {
		return PackageResult{}, err
	}
	serverURL, err := validatePackageServerURL(options.ServerURL)
	if err != nil {
		return PackageResult{}, err
	}
	if strings.TrimSpace(options.CLIPath) == "" {
		return PackageResult{}, errors.New("clipman-cli path is required")
	}
	cliPath, err := filepath.Abs(options.CLIPath)
	if err != nil {
		return PackageResult{}, err
	}
	token, err := readPackageToken(tokenFile)
	if err != nil {
		return PackageResult{}, err
	}
	client, err := packageHTTPClient(options.CACertificate)
	if err != nil {
		return PackageResult{}, err
	}
	health, err := readPackageHealth(ctx, client, serverURL)
	if err != nil {
		return PackageResult{}, err
	}
	if health.Status != "ok" {
		return PackageResult{}, fmt.Errorf("package health status is %q, expected ok", health.Status)
	}
	if strings.TrimSpace(health.Version) == "" {
		return PackageResult{}, errors.New("package health did not report a server version")
	}
	if options.ExpectedVersion != "" && health.Version != options.ExpectedVersion {
		return PackageResult{}, fmt.Errorf("installed server reports version %q, executable reports %q", health.Version, options.ExpectedVersion)
	}
	seed := strings.TrimSpace(options.Seed)
	if seed == "" {
		seed = "20260813"
	}
	seedDigest := sha256.Sum256([]byte(seed))
	name := "package-smoke-" + hex.EncodeToString(seedDigest[:6])
	password := "clipman-package-smoke-" + hex.EncodeToString(seedDigest[:])
	payload := []byte("Clipman package compatibility\nUnicode: café 東京 😀\nCombining: e\u0301\nCRLF:\r\nline\nNUL:\x00:end")
	payloadDigest := sha256.Sum256(payload)
	root, err := os.MkdirTemp(testRoot, "compat-client-")
	if err != nil {
		return PackageResult{}, err
	}
	defer os.RemoveAll(root)
	configPath := filepath.Join(root, "client", "config.toml")
	payloadPath := filepath.Join(root, "payload.txt")
	if err = os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return PackageResult{}, err
	}
	if err = os.WriteFile(payloadPath, payload, 0o600); err != nil {
		return PackageResult{}, err
	}
	base := []string{"--config", configPath, "--server", serverURL}
	if options.CACertificate != "" {
		caPath, absoluteErr := filepath.Abs(options.CACertificate)
		if absoluteErr != nil {
			return PackageResult{}, absoluteErr
		}
		base = append(base, "--ca-cert", caPath)
	}
	runner := packageCLIRunner{path: cliPath, password: password, secrets: []string{token, password}}
	if _, err = runner.run(ctx, append(append([]string{}, base...), "init", "--token-file", tokenFile, "--save-password", "none", "--machine", name, "--non-interactive", "--force")...); err != nil {
		return PackageResult{}, err
	}
	commandBase := []string{"--config", configPath, "--json"}
	if _, err = runner.run(ctx, append(append([]string{}, commandBase...), "put", "--file", payloadPath, "--name", name, "--group", "package-compat", "--duplicate", "keep")...); err != nil {
		return PackageResult{}, err
	}
	getOutput, err := runner.run(ctx, append(append([]string{}, commandBase...), "get", "--name", name)...)
	if err != nil {
		return PackageResult{}, err
	}
	var got packageEntry
	if err = json.Unmarshal(getOutput, &got); err != nil {
		return PackageResult{}, fmt.Errorf("decode package get output: %w", err)
	}
	if got.Name != name || got.Text != string(payload) {
		return PackageResult{}, errors.New("package get changed the test entry name or bytes")
	}
	listOutput, err := runner.run(ctx, append(append([]string{}, commandBase...), "list", "--all", "--kind", "history")...)
	if err != nil {
		return PackageResult{}, err
	}
	var entries []packageEntry
	if err = json.Unmarshal(listOutput, &entries); err != nil {
		return PackageResult{}, fmt.Errorf("decode package list output: %w", err)
	}
	found := 0
	for _, entry := range entries {
		if entry.Name == name && entry.Text == string(payload) {
			found++
		}
	}
	if found != 1 {
		return PackageResult{}, fmt.Errorf("package list contained %d exact test entries, expected 1", found)
	}
	if _, err = runner.run(ctx, append(append([]string{}, commandBase...), "status", "--refresh")...); err != nil {
		return PackageResult{}, err
	}
	if _, err = runner.run(ctx, append(append([]string{}, commandBase...), "sync")...); err != nil {
		return PackageResult{}, err
	}
	if _, err = runner.run(ctx, append(append([]string{}, commandBase...), "rm", "--name", name, "--yes")...); err != nil {
		return PackageResult{}, err
	}
	if _, err = runner.run(ctx, append(append([]string{}, commandBase...), "sync")...); err != nil {
		return PackageResult{}, err
	}
	return PackageResult{
		ServerVersion: health.Version,
		Seed:          seed,
		PayloadSHA256: hex.EncodeToString(payloadDigest[:]),
		Operations:    []string{"health", "init", "put", "get", "list", "status-refresh", "sync", "remove", "sync-after-remove"},
	}, nil
}

func validatePackageTestRoot(root, tokenFilename string) (string, string, error) {
	if strings.TrimSpace(root) == "" {
		return "", "", errors.New("package test root is required")
	}
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return "", "", err
	}
	stat, err := os.Stat(resolvedRoot)
	if err != nil {
		return "", "", err
	}
	if !stat.IsDir() {
		return "", "", errors.New("package test root is not a directory")
	}
	if !strings.HasPrefix(filepath.Base(resolvedRoot), ".test-tmp-clipman-server-package-") {
		return "", "", fmt.Errorf("refusing non-test package root %q", resolvedRoot)
	}
	markerData, err := os.ReadFile(filepath.Join(resolvedRoot, packageMarkerFilename))
	if err != nil {
		return "", "", fmt.Errorf("read package test marker: %w", err)
	}
	var marker struct {
		Purpose string `json:"purpose"`
		Version int    `json:"version"`
	}
	if err = json.Unmarshal(markerData, &marker); err != nil || marker.Purpose != "clipman-server-package-compat" || marker.Version != 1 {
		return "", "", errors.New("package test root does not contain a valid safety marker")
	}
	if strings.TrimSpace(tokenFilename) == "" {
		return "", "", errors.New("package token file is required")
	}
	absoluteToken, err := filepath.Abs(tokenFilename)
	if err != nil {
		return "", "", err
	}
	resolvedToken, err := filepath.EvalSymlinks(absoluteToken)
	if err != nil {
		return "", "", err
	}
	relative, err := filepath.Rel(resolvedRoot, resolvedToken)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", "", errors.New("package token file must be inside the marked test root")
	}
	return resolvedRoot, resolvedToken, nil
}

type packageCLIRunner struct {
	path     string
	password string
	secrets  []string
}

func (r packageCLIRunner) run(ctx context.Context, arguments ...string) ([]byte, error) {
	commandContext, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	command := exec.CommandContext(commandContext, r.path, arguments...)
	command.Env = packageEnvironment(r.password)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("clipman-cli %s timed out", packageCommandName(arguments))
	}
	if err != nil {
		diagnostic := strings.TrimSpace(stderr.String())
		if diagnostic == "" {
			diagnostic = strings.TrimSpace(stdout.String())
		}
		for _, secret := range r.secrets {
			if secret != "" {
				diagnostic = strings.ReplaceAll(diagnostic, secret, "<redacted>")
			}
		}
		if len(diagnostic) > 4096 {
			diagnostic = diagnostic[:4096] + "..."
		}
		return nil, fmt.Errorf("clipman-cli %s failed: %w: %s", packageCommandName(arguments), err, diagnostic)
	}
	return stdout.Bytes(), nil
}

func packageCommandName(arguments []string) string {
	for _, argument := range arguments {
		switch argument {
		case "init", "put", "get", "list", "status", "sync", "rm":
			return argument
		}
	}
	return "command"
}

func packageEnvironment(password string) []string {
	environment := make([]string, 0, len(os.Environ())+1)
	for _, value := range os.Environ() {
		name, _, _ := strings.Cut(value, "=")
		if strings.EqualFold(name, "CLIPMAN_PASSWORD") {
			continue
		}
		environment = append(environment, value)
	}
	return append(environment, "CLIPMAN_PASSWORD="+password)
}

func validatePackageServerURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return "", fmt.Errorf("invalid package server URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("package server URL must use http or https")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("package server URL must contain only a scheme, host, and optional port")
	}
	host := parsed.Hostname()
	address := net.ParseIP(strings.Trim(host, "[]"))
	if !strings.EqualFold(host, "localhost") && (address == nil || !address.IsLoopback()) {
		return "", errors.New("package mode only targets a loopback endpoint")
	}
	if parsed.Host == "" {
		return "", errors.New("package server URL has no host")
	}
	parsed.Path = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func readPackageToken(filename string) (string, error) {
	if strings.TrimSpace(filename) == "" {
		return "", errors.New("package token file is required")
	}
	stat, err := os.Stat(filename)
	if err != nil {
		return "", err
	}
	if stat.Size() <= 0 || stat.Size() > maxPackageTokenBytes {
		return "", errors.New("package token file has an invalid size")
	}
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return "", errors.New("package token file contains an invalid token")
	}
	return token, nil
}

func packageHTTPClient(caFilename string) (*http.Client, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	if caFilename != "" {
		data, err := os.ReadFile(caFilename)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return nil, errors.New("package CA certificate did not contain a certificate")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	return &http.Client{Transport: transport, Timeout: 15 * time.Second}, nil
}

func readPackageHealth(ctx context.Context, client *http.Client, serverURL string) (packageHealth, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, serverURL+"/api/v1/health", nil)
	if err != nil {
		return packageHealth{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return packageHealth{}, fmt.Errorf("package health request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return packageHealth{}, fmt.Errorf("package health returned %s", response.Status)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxPackageHealthBytes+1))
	if err != nil {
		return packageHealth{}, err
	}
	if len(data) > maxPackageHealthBytes {
		return packageHealth{}, errors.New("package health response was unexpectedly large")
	}
	var health packageHealth
	if err = json.Unmarshal(data, &health); err != nil {
		return packageHealth{}, fmt.Errorf("decode package health response: %w", err)
	}
	return health, nil
}
