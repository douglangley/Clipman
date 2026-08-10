package onboarding

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
)

const StateFilename = "clipman-server-setup-link.json"

type Connection struct {
	Clipman   string `json:"clipman"`
	Version   int    `json:"version"`
	Address   string `json:"address"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Token     string `json:"token"`
	CACertPEM string `json:"ca_cert_pem,omitempty"`
}
type State struct {
	Version            int    `json:"version"`
	CodeSHA256         string `json:"code_sha256"`
	CreatedUnixMS      int64  `json:"created_unix_ms"`
	ExpiresUnixMS      int64  `json:"expires_unix_ms"`
	RemainingDownloads int    `json:"remaining_downloads"`
}
type Manager struct {
	ConfigPath string
	Now        func() time.Time
	mu         sync.Mutex
}

func ConnectionDocument(settings config.Settings) (Connection, error) {
	host := strings.TrimSpace(settings.String("AdvertiseHost"))
	if host == "" {
		host = strings.Trim(settings.String("Host"), "[]")
	}
	port, err := settings.Int("Port")
	if err != nil {
		return Connection{}, err
	}
	scheme := "clipman"
	ca := ""
	if settings.String("CertFile") != "" {
		scheme = "https"
		caPath := settings.String("CaFile")
		if caPath != "" {
			data, readErr := os.ReadFile(caPath)
			if readErr != nil {
				return Connection{}, readErr
			}
			ca = string(data)
		}
	}
	urlHost := host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		urlHost = "[" + host + "]"
	}
	return Connection{Clipman: "server-connection", Version: 1, Address: fmt.Sprintf("%s://%s:%d", scheme, urlHost, port), Host: host, Port: port, Token: settings.String("AuthToken"), CACertPEM: ca}, nil
}
func ConnectionBytes(settings config.Settings) ([]byte, error) {
	doc, err := ConnectionDocument(settings)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
func WriteConnectionFiles(configPath string, settings config.Settings) (string, string, error) {
	data, err := ConnectionBytes(settings)
	if err != nil {
		return "", "", err
	}
	dir := filepath.Dir(configPath)
	jsonPath := filepath.Join(dir, "clipman-server-connection.clpconf")
	textPath := filepath.Join(dir, "clipman-server-connection.txt")
	if err = os.WriteFile(jsonPath, data, 0o600); err != nil {
		return "", "", err
	}
	doc, _ := ConnectionDocument(settings)
	text := fmt.Sprintf("Clipman Server connection details\n\nServer address: %s\nPort: %d\nToken: %s\n\nKeep this file private. After every Clipman client has been configured,\ndelete this file or move the details to your password manager.\n", doc.Address, doc.Port, doc.Token)
	if err = os.WriteFile(textPath, []byte(text), 0o600); err != nil {
		return "", "", err
	}
	return textPath, jsonPath, nil
}

func (m *Manager) Create(minutes, downloads int) (string, State, error) {
	if minutes < 1 || minutes > 1440 {
		return "", State{}, errors.New("setup link minutes must be between 1 and 1440")
	}
	if downloads < 1 || downloads > 50 {
		return "", State{}, errors.New("setup link downloads must be between 1 and 50")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", State{}, err
	}
	code := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(code))
	now := m.now()
	state := State{1, hex.EncodeToString(sum[:]), now.UnixMilli(), now.Add(time.Duration(minutes) * time.Minute).UnixMilli(), downloads}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.save(state); err != nil {
		return "", State{}, err
	}
	return code, state, nil
}
func (m *Manager) Revoke() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	err := os.Remove(m.path())
	return err == nil
}
func (m *Manager) Lookup(code string, consume bool) (State, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, err := os.ReadFile(m.path())
	if err != nil {
		return State{}, false
	}
	var state State
	if json.Unmarshal(data, &state) != nil {
		return State{}, false
	}
	sum := sha256.Sum256([]byte(code))
	actual := hex.EncodeToString(sum[:])
	if len(actual) != len(state.CodeSHA256) || subtle.ConstantTimeCompare([]byte(actual), []byte(state.CodeSHA256)) != 1 {
		return State{}, false
	}
	if state.ExpiresUnixMS <= m.now().UnixMilli() || state.RemainingDownloads <= 0 {
		_ = os.Remove(m.path())
		return State{}, false
	}
	if consume {
		state.RemainingDownloads--
		if state.RemainingDownloads == 0 {
			_ = os.Remove(m.path())
		} else if m.save(state) != nil {
			return State{}, false
		}
	}
	return state, true
}
func (m *Manager) path() string { return filepath.Join(filepath.Dir(m.ConfigPath), StateFilename) }
func (m *Manager) save(state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp := m.path() + ".tmp"
	if err = os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path())
}
func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func SetupPage(code string, state State, userAgent string) []byte {
	suggestion := "Download the connection file on the device where you want to configure Clipman."
	agent := strings.ToLower(userAgent)
	if strings.Contains(agent, "windows") {
		suggestion = "On this Windows computer, download the connection file, then import it from Clipman Preferences."
	} else if strings.Contains(agent, "android") {
		suggestion = "On this Android device, download the connection file and open it with Clipman."
	}
	body := fmt.Sprintf(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Connect Clipman to this server</title></head><body><main><h1>Connect Clipman to this server</h1><p>%s</p><p><a href="/setup/%s/connection.clpconf" download>Download Clipman server connection</a></p><p><strong>Keep the downloaded file private.</strong> It contains the server token. It does not contain your clipboard history password.</p><p>This temporary page has %d connection-file downloads remaining.</p></main></body></html>`, html.EscapeString(suggestion), html.EscapeString(code), state.RemainingDownloads)
	return []byte(body)
}
