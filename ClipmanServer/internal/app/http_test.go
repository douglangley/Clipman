package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/blobstore"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/onboarding"
)

func TestDatabaseHTTPRoundTripAndConditions(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", newRuntimeStats(), store)
	id := strings.Repeat("a", 43)
	do := func(method string, body []byte, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v1/database/"+id, bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	created := do(http.MethodPut, []byte("clipdb"), map[string]string{"If-None-Match": "*"})
	if created.Code != 200 || created.Header().Get("X-Clipman-Revision") == "" {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	revision := created.Header().Get("X-Clipman-Revision")
	head := do(http.MethodHead, nil, nil)
	if head.Code != 200 || head.Header().Get("Content-Length") != "6" || head.Header().Get("X-Clipman-Revision") != revision {
		t.Fatalf("head: %d %v", head.Code, head.Header())
	}
	get := do(http.MethodGet, nil, nil)
	if get.Code != 200 || get.Body.String() != "clipdb" {
		t.Fatalf("get: %d %q", get.Code, get.Body.String())
	}
	conflict := do(http.MethodPut, []byte("changed"), map[string]string{"If-Match": "wrong"})
	if conflict.Code != http.StatusConflict || conflict.Header().Get("X-Clipman-Revision") != revision {
		t.Fatalf("conflict: %d %s", conflict.Code, conflict.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload["Status"] != "ok" || payload["Runtime"] == nil || payload["ListenPrefix"] == nil {
		t.Fatalf("invalid response JSON: %s (%v)", created.Body.String(), err)
	}
}

func TestDatabaseHTTPRequiresAuthentication(t *testing.T) {
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	store, _ := blobstore.New(blobstore.Options{Root: t.TempDir(), MaxDatabaseBytes: 1024})
	req := httptest.NewRequest(http.MethodHead, "/api/v1/database/"+strings.Repeat("a", 43), nil)
	response := httptest.NewRecorder()
	newHandler(settings, filepath.Join(t.TempDir(), "settings.json"), "test", newRuntimeStats(), store).ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", response.Code)
	}
}

func TestSetupLinkDownloadIsLimitedAndUnauthenticated(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "setup-secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	configPath := filepath.Join(root, "settings.json")
	manager := &onboarding.Manager{ConfigPath: configPath}
	code, _, err := manager.Create(5, 1)
	if err != nil {
		t.Fatal(err)
	}
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	handler := newHandler(settings, configPath, "test", newRuntimeStats(), store)
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/setup/"+code, nil))
	if page.Code != 200 || strings.Contains(page.Body.String(), "setup-secret") {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	download := httptest.NewRecorder()
	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/setup/"+code+"/connection.clpconf", nil))
	if head.Code != 200 || head.Body.Len() != 0 {
		t.Fatalf("head status=%d body=%q", head.Code, head.Body.String())
	}
	handler.ServeHTTP(download, httptest.NewRequest(http.MethodGet, "/setup/"+code+"/connection.clpconf", nil))
	if download.Code != 200 || !strings.Contains(download.Body.String(), "setup-secret") {
		t.Fatalf("download status=%d body=%s", download.Code, download.Body.String())
	}
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/setup/"+code+"/connection.clpconf", nil))
	if second.Code != 404 {
		t.Fatalf("second download status=%d", second.Code)
	}
}

func TestHealthMethodCompatibilityBoundary(t *testing.T) {
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	root := t.TempDir()
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", newRuntimeStats(), store)
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodHead, "/api/v1/health", nil))
	if unauthorized.Code != 401 {
		t.Fatalf("unauthenticated HEAD status=%d", unauthorized.Code)
	}
	request := httptest.NewRequest(http.MethodHead, "/api/v1/health", nil)
	request.Header.Set("Authorization", "Bearer secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, request)
	if authorized.Code != 404 {
		t.Fatalf("authenticated HEAD status=%d", authorized.Code)
	}
}

func TestLegacyBackupAdministrationRoutes(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	backupDir := filepath.Join(root, "Databases", strings.Repeat("b", 43), "ServerBackups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "one.clipdb"), []byte("backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", newRuntimeStats(), store)
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		return response
	}
	listed := request(http.MethodGet, "/api/v1/backups")
	if listed.Code != 200 || !strings.Contains(listed.Body.String(), `"Name": "one.clipdb"`) || strings.Contains(listed.Body.String(), strings.Repeat("b", 43)) {
		t.Fatalf("backup list status=%d body=%s", listed.Code, listed.Body.String())
	}
	for _, item := range []struct{ method, path, message string }{
		{http.MethodPost, "/api/v1/backup", "Use a database-scoped backup endpoint"},
		{http.MethodPost, "/api/v1/restore?name=one.clipdb", "Use a database-scoped restore endpoint"},
	} {
		response := request(item.method, item.path)
		if response.Code != 404 || response.Body.String() != item.message {
			t.Fatalf("%s status=%d body=%q", item.path, response.Code, response.Body.String())
		}
	}
}

func TestMalformedDatabaseRequestsDoNotCreateBuckets(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	settings.SetInt("MaxDatabaseBytes", 8)
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 8})
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", newRuntimeStats(), store)
	id := strings.Repeat("a", 43)
	cases := []struct {
		name, path    string
		contentLength int64
		headers       map[string]string
		want          int
	}{{"encoded separator", "/api/v1/database/" + id + "%2Fbad", 0, nil, 404}, {"missing length", "/api/v1/database/" + id, -1, nil, 400}, {"oversize", "/api/v1/database/" + id, 9, nil, 413}, {"bad create condition", "/api/v1/database/" + id, 1, map[string]string{"If-None-Match": "value"}, 400}, {"combined conditions", "/api/v1/database/" + id, 1, map[string]string{"If-None-Match": "*", "If-Match": "revision"}, 400}}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, item.path, strings.NewReader("123456789"))
			request.ContentLength = item.contentLength
			request.Header.Set("Authorization", "Bearer secret")
			for key, value := range item.headers {
				request.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != item.want {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}
	entries, err := os.ReadDir(filepath.Join(root, "Databases"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed probes created %d buckets", len(entries))
	}
}

func TestConcurrentFirstWritersHaveOneWinner(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	stats := newRuntimeStats()
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", stats, store)
	id := strings.Repeat("c", 43)
	const writers = 12
	statuses := make(chan int, writers)
	var wg sync.WaitGroup
	for index := 0; index < writers; index++ {
		wg.Add(1)
		go func(value byte) {
			defer wg.Done()
			request := httptest.NewRequest(http.MethodPut, "/api/v1/database/"+id, bytes.NewReader([]byte{value}))
			request.Header.Set("Authorization", "Bearer secret")
			request.Header.Set("If-None-Match", "*")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			statuses <- response.Code
		}(byte(index))
	}
	wg.Wait()
	close(statuses)
	success, precondition := 0, 0
	for status := range statuses {
		if status == 200 {
			success++
		} else if status == 412 {
			precondition++
		} else {
			t.Fatalf("unexpected status %d", status)
		}
	}
	if success != 1 || precondition != writers-1 {
		t.Fatalf("success=%d precondition=%d", success, precondition)
	}
	summary := stats.summary()
	if summary["DatabaseUploads"] != int64(writers) || summary["Conflicts"] != int64(writers-1) {
		t.Fatalf("stats=%+v", summary)
	}
}

func TestRuntimeCountersTrackDatabaseTraffic(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	stats := newRuntimeStats()
	handler := newHandler(settings, filepath.Join(root, "settings.json"), "test", stats, store)
	id := strings.Repeat("d", 43)
	request := func(method string, body string) {
		req := httptest.NewRequest(method, "/api/v1/database/"+id, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer secret")
		if method == http.MethodPut {
			req.Header.Set("If-None-Match", "*")
		}
		handler.ServeHTTP(httptest.NewRecorder(), req)
	}
	request(http.MethodPut, "abcd")
	request(http.MethodHead, "")
	request(http.MethodGet, "")
	summary := stats.summary()
	if summary["DatabaseUploads"] != int64(1) || summary["DatabasePolls"] != int64(1) || summary["DatabaseDownloads"] != int64(1) || summary["BytesReceived"] != int64(4) || summary["BytesSent"].(int64) < 4 || summary["UniqueClients"] != 1 {
		t.Fatalf("stats=%+v", summary)
	}
}
