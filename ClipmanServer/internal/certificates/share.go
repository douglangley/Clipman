package certificates

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

func Share(ctx context.Context, caPath, bindHost, advertisedHost string, minutes int, ready func(string, string)) error {
	if minutes < 1 || minutes > 60 {
		return errors.New("share minutes must be between 1 and 60")
	}
	data, err := os.ReadFile(caPath)
	if err != nil {
		return err
	}
	if !strings.Contains(string(data), "BEGIN CERTIFICATE") || strings.Contains(string(data), "PRIVATE KEY") {
		return errors.New("configured authority file is not a public PEM certificate")
	}
	certificate, _, err := readCertificate(caPath)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(certificate.Raw)
	listener, err := net.Listen("tcp", net.JoinHostPort(strings.Trim(bindHost, "[]"), "0"))
	if err != nil {
		return err
	}
	defer listener.Close()
	raw := make([]byte, 18)
	if _, err = rand.Read(raw); err != nil {
		return err
	}
	path := "/clipman-ca-" + base64.RawURLEncoding.EncodeToString(raw) + ".crt"
	downloaded := make(chan struct{}, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="clipman-server-ca.crt"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		w.WriteHeader(200)
		if r.Method == http.MethodGet {
			_, _ = w.Write(data)
			select {
			case downloaded <- struct{}{}:
			default:
			}
		}
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	host := advertisedHost
	if host == "" {
		host = bindHost
	}
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if ready != nil {
		ready(fmt.Sprintf("http://%s:%d%s", host, port, path), fingerprint(sum[:]))
	}
	timer := time.NewTimer(time.Duration(minutes) * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		err = ctx.Err()
	case <-timer.C:
	case <-downloaded:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	<-done
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
