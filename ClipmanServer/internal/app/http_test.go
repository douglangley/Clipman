package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/blobstore"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
)

func TestDatabaseHTTPRoundTripAndConditions(t *testing.T) {
	root := t.TempDir()
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	settings.SetString("DatabasePath", filepath.Join(root, "clipman-history.clipdb"))
	store, _ := blobstore.New(blobstore.Options{Root: root, MaxDatabaseBytes: 1024})
	handler := newHandler(settings, "test", newRuntimeStats(), store)
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
	if err := json.Unmarshal(created.Body.Bytes(), &payload); err != nil || payload["Status"] != "ok" {
		t.Fatalf("invalid response JSON: %s (%v)", created.Body.String(), err)
	}
}

func TestDatabaseHTTPRequiresAuthentication(t *testing.T) {
	settings, _ := config.Defaults()
	settings.SetString("AuthToken", "secret")
	store, _ := blobstore.New(blobstore.Options{Root: t.TempDir(), MaxDatabaseBytes: 1024})
	req := httptest.NewRequest(http.MethodHead, "/api/v1/database/"+strings.Repeat("a", 43), nil)
	response := httptest.NewRecorder()
	newHandler(settings, "test", newRuntimeStats(), store).ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", response.Code)
	}
}
