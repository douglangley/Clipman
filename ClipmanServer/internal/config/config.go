package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/platform"
)

const (
	ServerPortMin = 20000
	ServerPortMax = 49151
)

type Settings struct {
	Values map[string]any
}

type LoadResult struct {
	Settings Settings
	Created  bool
}

func Load(path string) (LoadResult, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		settings, defaultErr := Defaults()
		if defaultErr != nil {
			return LoadResult{}, defaultErr
		}
		if saveErr := Save(path, settings); saveErr != nil {
			return LoadResult{}, saveErr
		}
		return LoadResult{Settings: settings, Created: true}, nil
	}
	if err != nil {
		return LoadResult{}, err
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	values := map[string]any{}
	if err = decoder.Decode(&values); err != nil {
		return LoadResult{}, fmt.Errorf("decode settings: %w", err)
	}
	if err = requireEOF(decoder); err != nil {
		return LoadResult{}, err
	}
	defaults, err := Defaults()
	if err != nil {
		return LoadResult{}, err
	}
	for key, value := range defaults.Values {
		if _, exists := values[key]; !exists {
			values[key] = value
		}
	}
	return LoadResult{Settings: Settings{Values: values}}, nil
}

func Defaults() (Settings, error) {
	dataDir, err := platform.DefaultDataDir()
	if err != nil {
		return Settings{}, err
	}
	logPath, err := platform.DefaultLogPath()
	if err != nil {
		return Settings{}, err
	}
	port, err := FindAvailablePort()
	if err != nil {
		return Settings{}, err
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		return Settings{}, fmt.Errorf("generate authentication token: %w", err)
	}
	return Settings{Values: map[string]any{
		"Host":                          "127.0.0.1",
		"AdvertiseHost":                 "",
		"Port":                          json.Number(strconv.Itoa(port)),
		"DatabasePath":                  filepath.Join(dataDir, "clipman-history.clipdb"),
		"AuthToken":                     base64.RawURLEncoding.EncodeToString(tokenBytes),
		"LogPath":                       logPath,
		"CertFile":                      "",
		"KeyFile":                       "",
		"CaFile":                        "",
		"AllowInsecureRemote":           false,
		"SetupBaseUrl":                  "",
		"BackupIntervalMinutes":         json.Number("60"),
		"BackupRetentionHours":          json.Number("24"),
		"MaxBackups":                    json.Number("48"),
		"CreateBackupBeforeEveryUpload": true,
		"DatabasePruneDays":             json.Number("0"),
		"DatabasePruneIntervalHours":    json.Number("24"),
		"MaxDatabaseBytes":              json.Number(strconv.Itoa(64 * 1024 * 1024)),
	}}, nil
}

func FindAvailablePort() (int, error) {
	span := ServerPortMax - ServerPortMin + 1
	seedBytes := make([]byte, 2)
	if _, err := rand.Read(seedBytes); err != nil {
		return 0, err
	}
	start := (int(seedBytes[0])<<8 | int(seedBytes[1])) % span
	for attempt := 0; attempt < 256; attempt++ {
		port := ServerPortMin + (start+attempt)%span
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		_ = listener.Close()
		return port, nil
	}
	return 0, errors.New("could not find an available persistent server port")
}

func Save(path string, settings Settings) error {
	data, err := Marshal(settings.Values)
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err = temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	return os.Chmod(path, 0o600)
}

func Marshal(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	data := bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
	return escapeNonASCII(data), nil
}

func escapeNonASCII(data []byte) []byte {
	var result strings.Builder
	result.Grow(len(data))
	for _, r := range string(data) {
		if r <= 0x7f {
			result.WriteRune(r)
			continue
		}
		if r <= 0xffff {
			fmt.Fprintf(&result, "\\u%04x", r)
			continue
		}
		high, low := utf16.EncodeRune(r)
		fmt.Fprintf(&result, "\\u%04x\\u%04x", high, low)
	}
	return []byte(result.String())
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("settings contain more than one JSON value")
	}
	return err
}

func (s Settings) String(key string) string {
	value, _ := s.Values[key].(string)
	return value
}

func (s Settings) Bool(key string) bool {
	value, _ := s.Values[key].(bool)
	return value
}

func (s Settings) Int(key string) (int, error) {
	switch value := s.Values[key].(type) {
	case json.Number:
		parsed, err := value.Int64()
		return int(parsed), err
	case float64:
		return int(value), nil
	case int:
		return value, nil
	default:
		return 0, fmt.Errorf("setting %s must be an integer", key)
	}
}

func (s Settings) SetString(key, value string)    { s.Values[key] = value }
func (s Settings) SetBool(key string, value bool) { s.Values[key] = value }
func (s Settings) SetInt(key string, value int) {
	s.Values[key] = json.Number(strconv.Itoa(value))
}
