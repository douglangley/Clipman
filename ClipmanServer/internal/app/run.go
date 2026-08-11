package app

import (
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/OnjLouis/Clipman/ClipmanServer/internal/admin"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/blobstore"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/certificates"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/config"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/dataroot"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/onboarding"
	"github.com/OnjLouis/Clipman/ClipmanServer/internal/platform"
)

const (
	bindErrorExitCode    = 20
	dataRootLockExitCode = 21
	shutdownTimeout      = 10 * time.Second
)

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

type options struct {
	showVersion, suggestPort, allowInsecureRemote, createTLSCertificate bool
	showHost, showDatabasePruneDays                                     bool
	newCA, showCAFingerprint, shareCA, showToken                        bool
	writeConnectionInfo, createSetupLink, revokeSetupLink               bool
	listDatabases, listDatabasesJSON, confirm, forceRecent              bool
	configPath, host, advertiseHost, databasePath, logPath              string
	certFile, keyFile, shareHost, setupBaseURL, deleteDatabase          string
	port, shareMinutes, setupMinutes, setupDownloads, pruneDays         int
	portSet, pruneDaysSet                                               bool
	certHosts, certIPs                                                  stringList
}

func Run(args []string, version string, stdout, stderr io.Writer) int {
	workingDirectory, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defaultConfig, err := platform.DefaultConfigPath(workingDirectory)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	opts, err := parseOptions(args, defaultConfig)
	if errors.Is(err, flag.ErrHelp) {
		printUsage(stdout)
		return 0
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if opts.showVersion {
		fmt.Fprintln(stdout, version)
		return 0
	}
	if opts.suggestPort {
		port, findErr := config.FindAvailablePort()
		if findErr != nil {
			fmt.Fprintln(stderr, findErr)
			return 1
		}
		fmt.Fprintln(stdout, port)
		return 0
	}
	if opts.portSet && (opts.port < 1 || opts.port > 65535) {
		fmt.Fprintln(stderr, "The listening port must be between 1 and 65535.")
		return 2
	}

	configPath, err := absolutePath(opts.configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Could not resolve settings path: %v\n", err)
		return 1
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(stderr, "Could not load Clipman Server settings: %v\n", err)
		return 1
	}
	settings := loaded.Settings
	changed := applyOverrides(settings, opts)
	if changed {
		if err = config.Save(configPath, settings); err != nil {
			fmt.Fprintf(stderr, "Could not save Clipman Server settings: %v\n", err)
			return 1
		}
	}
	if opts.showToken {
		fmt.Fprintln(stdout, settings.String("AuthToken"))
		return 0
	}
	if opts.showHost {
		fmt.Fprintln(stdout, settings.String("Host"))
		return 0
	}
	if opts.showDatabasePruneDays {
		days, _ := settings.Int("DatabasePruneDays")
		fmt.Fprintln(stdout, days)
		return 0
	}
	if opts.createTLSCertificate {
		hosts := append([]string{settings.String("Host"), settings.String("AdvertiseHost")}, opts.certHosts...)
		result, certErr := certificates.Generate(configPath, hosts, opts.certIPs, opts.newCA)
		if certErr != nil {
			fmt.Fprintf(stderr, "Could not create the HTTPS certificate: %v\n", certErr)
			return 1
		}
		settings.SetString("CaFile", result.Authority)
		settings.SetString("CertFile", result.FullChain)
		settings.SetString("KeyFile", result.Key)
		if saveErr := config.Save(configPath, settings); saveErr != nil {
			fmt.Fprintln(stderr, saveErr)
			return 1
		}
		_, _, _ = onboarding.WriteConnectionFiles(configPath, settings)
		fmt.Fprintf(stdout, "Certificate authority: %s\nServer certificate: %s\nServer certificate expires: %s\nAuthority SHA-256 fingerprint: %s\n", result.Authority, result.Certificate, result.Expires.UTC().Format(time.RFC1123), result.Fingerprint)
		return 0
	}
	if opts.showCAFingerprint {
		ca := settings.String("CaFile")
		cert := settings.String("CertFile")
		host := settings.String("AdvertiseHost")
		if host == "" {
			host = settings.String("Host")
		}
		fingerprint, _, inspectErr := certificates.InspectCA(ca, cert, host)
		if inspectErr != nil {
			fmt.Fprintf(stderr, "Could not inspect the private certificate authority: %v\n", inspectErr)
			return 1
		}
		fmt.Fprintln(stdout, fingerprint)
		return 0
	}
	if opts.shareCA {
		bindHost := opts.shareHost
		if bindHost == "" {
			bindHost = settings.String("Host")
		}
		advertised := settings.String("AdvertiseHost")
		if advertised == "" {
			advertised = bindHost
		}
		shareErr := certificates.Share(context.Background(), settings.String("CaFile"), bindHost, advertised, opts.shareMinutes, func(url, fingerprint string) {
			fmt.Fprintf(stdout, "Certificate URL: %s\nSHA-256 fingerprint: %s\nSharing stops after the first download or %d minute(s).\n", url, fingerprint, opts.shareMinutes)
		})
		if shareErr != nil {
			fmt.Fprintf(stderr, "Could not share the certificate authority: %v\n", shareErr)
			return 1
		}
		fmt.Fprintln(stdout, "Certificate sharing stopped: download completed or time limit reached.")
		return 0
	}
	if opts.writeConnectionInfo {
		_, connectionPath, writeErr := onboarding.WriteConnectionFiles(configPath, settings)
		if writeErr != nil {
			fmt.Fprintf(stderr, "Could not write the Clipman Server connection files: %v\n", writeErr)
			return 1
		}
		fmt.Fprintln(stdout, connectionPath)
		return 0
	}
	setupManager := &onboarding.Manager{ConfigPath: configPath}
	if opts.createSetupLink {
		code, state, createErr := setupManager.Create(opts.setupMinutes, opts.setupDownloads)
		if createErr != nil {
			fmt.Fprintf(stderr, "Could not create the temporary setup link: %v\n", createErr)
			return 1
		}
		base := strings.TrimRight(settings.String("SetupBaseUrl"), "/")
		if base == "" {
			scheme := "http"
			if settings.String("CertFile") != "" {
				scheme = "https"
			}
			host := settings.String("AdvertiseHost")
			if host == "" {
				host = settings.String("Host")
			}
			port, _ := settings.Int("Port")
			base = fmt.Sprintf("%s://%s:%d", scheme, host, port)
		}
		fmt.Fprintf(stdout, "Setup URL: %s/setup/%s\nExpires: %s\nConnection-file downloads: %d\nRevoke early with --revoke-setup-link.\n", base, code, time.UnixMilli(state.ExpiresUnixMS).UTC().Format("2006-01-02 15:04 UTC"), state.RemainingDownloads)
		return 0
	}
	if opts.revokeSetupLink {
		if setupManager.Revoke() {
			fmt.Fprintln(stdout, "Temporary setup link revoked.")
		} else {
			fmt.Fprintln(stdout, "No temporary setup link is active.")
		}
		return 0
	}
	if unsupportedAction(opts) {
		fmt.Fprintln(stderr, "This administration operation is not implemented in the Go compatibility server yet.")
		return 1
	}
	if err = validateSettings(settings); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	lock, err := dataroot.Acquire(filepath.Dir(settings.String("DatabasePath")))
	if err != nil {
		fmt.Fprintf(stderr, "Another Clipman Server process is already using the data root %s. Stop that server before starting another one with the same data root.\n", filepath.Dir(settings.String("DatabasePath")))
		return dataRootLockExitCode
	}
	defer lock.Close()
	if opts.listDatabases || opts.listDatabasesJSON || opts.deleteDatabase != "" || opts.pruneDaysSet {
		return runDatabaseAdministration(settings, opts, stdout, stderr)
	}
	return runHTTP(settings, configPath, version, stdout, stderr)
}

func parseOptions(args []string, defaultConfig string) (options, error) {
	var result options
	result.configPath = defaultConfig
	result.shareMinutes = 10
	result.setupMinutes = 30
	result.setupDownloads = 5
	set := flag.NewFlagSet("clipman-server", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	set.BoolVar(&result.showVersion, "version", false, "print version")
	set.StringVar(&result.configPath, "config", defaultConfig, "settings path")
	set.StringVar(&result.host, "host", "", "listen host")
	set.StringVar(&result.advertiseHost, "advertise-host", "", "client host")
	set.IntVar(&result.port, "port", 0, "listen port")
	set.BoolVar(&result.suggestPort, "suggest-port", false, "suggest port")
	set.StringVar(&result.databasePath, "database", "", "database path")
	set.StringVar(&result.logPath, "log", "", "log path")
	set.StringVar(&result.certFile, "cert-file", "", "TLS certificate")
	set.StringVar(&result.keyFile, "key-file", "", "TLS key")
	set.BoolVar(&result.createTLSCertificate, "create-tls-certificate", false, "create TLS certificate")
	set.Var(&result.certHosts, "cert-host", "certificate DNS name")
	set.Var(&result.certIPs, "cert-ip", "certificate IP")
	set.BoolVar(&result.newCA, "new-ca", false, "replace private CA")
	set.BoolVar(&result.showCAFingerprint, "show-ca-fingerprint", false, "show CA fingerprint")
	set.BoolVar(&result.shareCA, "share-ca", false, "share CA")
	set.IntVar(&result.shareMinutes, "share-minutes", 10, "share duration")
	set.StringVar(&result.shareHost, "share-host", "", "share host")
	set.BoolVar(&result.allowInsecureRemote, "allow-insecure-remote", false, "allow remote HTTP")
	set.BoolVar(&result.showToken, "show-token", false, "show token")
	set.BoolVar(&result.showHost, "show-host", false, "show configured listen host")
	set.BoolVar(&result.showDatabasePruneDays, "show-database-prune-days", false, "show configured database prune age")
	set.BoolVar(&result.writeConnectionInfo, "write-connection-info", false, "write connection files")
	set.BoolVar(&result.createSetupLink, "create-setup-link", false, "create setup link")
	set.BoolVar(&result.revokeSetupLink, "revoke-setup-link", false, "revoke setup link")
	set.IntVar(&result.setupMinutes, "setup-minutes", 30, "setup duration")
	set.IntVar(&result.setupDownloads, "setup-downloads", 5, "setup downloads")
	set.StringVar(&result.setupBaseURL, "setup-base-url", "", "setup base URL")
	set.BoolVar(&result.listDatabases, "list-databases", false, "list databases")
	set.BoolVar(&result.listDatabasesJSON, "list-databases-json", false, "list databases as JSON")
	set.StringVar(&result.deleteDatabase, "delete-database", "", "delete database")
	set.IntVar(&result.pruneDays, "prune-databases-days", 0, "prune database age")
	set.BoolVar(&result.confirm, "confirm", false, "confirm maintenance")
	set.BoolVar(&result.forceRecent, "force-recent", false, "allow recent database deletion")
	if err := set.Parse(args); err != nil {
		return options{}, err
	}
	if set.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %s", strings.Join(set.Args(), " "))
	}
	set.Visit(func(item *flag.Flag) {
		switch item.Name {
		case "port":
			result.portSet = true
		case "prune-databases-days":
			result.pruneDaysSet = true
		}
	})
	return result, nil
}

func applyOverrides(settings config.Settings, opts options) bool {
	changed := false
	setString := func(key, value string, supplied bool) {
		if supplied && settings.String(key) != value {
			settings.SetString(key, value)
			changed = true
		}
	}
	setString("Host", opts.host, opts.host != "")
	setString("AdvertiseHost", opts.advertiseHost, opts.advertiseHost != "")
	if opts.portSet {
		current, _ := settings.Int("Port")
		if current != opts.port {
			settings.SetInt("Port", opts.port)
			changed = true
		}
	}
	for key, value := range map[string]string{
		"DatabasePath": opts.databasePath,
		"LogPath":      opts.logPath,
		"CertFile":     opts.certFile,
		"KeyFile":      opts.keyFile,
		"SetupBaseUrl": opts.setupBaseURL,
	} {
		if value == "" {
			continue
		}
		absolute, err := absolutePath(value)
		if key == "SetupBaseUrl" {
			absolute, err = value, nil
		}
		if err == nil && settings.String(key) != absolute {
			settings.SetString(key, absolute)
			changed = true
		}
	}
	if opts.allowInsecureRemote && !settings.Bool("AllowInsecureRemote") {
		settings.SetBool("AllowInsecureRemote", true)
		changed = true
	}
	return changed
}

func unsupportedAction(opts options) bool {
	return opts.shareHost != "" || len(opts.certHosts) != 0 || len(opts.certIPs) != 0 || opts.newCA
}

func runDatabaseAdministration(settings config.Settings, opts options, stdout, stderr io.Writer) int {
	manager := admin.Manager{Root: filepath.Dir(settings.String("DatabasePath"))}
	if opts.listDatabases || opts.listDatabasesJSON {
		items, err := manager.List()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		if opts.listDatabasesJSON {
			data, _ := config.Marshal(map[string]any{"Databases": items})
			_, _ = stdout.Write(data)
			fmt.Fprintln(stdout)
			return 0
		}
		if len(items) == 0 {
			fmt.Fprintln(stdout, "No Clipman Server database buckets found.")
			return 0
		}
		fmt.Fprintln(stdout, "Database buckets:")
		for _, item := range items {
			id := item.DatabaseID
			if len(id) > 15 {
				id = id[:12] + "..."
			}
			fmt.Fprintf(stdout, "%s  size=%d bytes  backups=%d\n", id, item.Length, item.BackupCount)
		}
		fmt.Fprintln(stdout, "\nUse --list-databases-json for full IDs and exact timestamps.")
		return 0
	}
	if opts.deleteDatabase != "" {
		if !opts.confirm {
			fmt.Fprintln(stderr, "Refusing to move a database bucket without --confirm. This is intentionally not automatic.")
			return 1
		}
		target, err := manager.Delete(opts.deleteDatabase, opts.forceRecent)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "Moved database bucket to %s\n", target)
		return 0
	}
	items, err := manager.Stale(opts.pruneDays)
	if err != nil {
		fmt.Fprintln(stderr, "--prune-databases-days must be greater than zero.")
		return 1
	}
	if len(items) == 0 {
		fmt.Fprintf(stdout, "No database buckets older than %d days.\n", opts.pruneDays)
		return 0
	}
	if !opts.confirm {
		fmt.Fprintf(stdout, "Database buckets older than %d days that would be moved:\n", opts.pruneDays)
		for _, item := range items {
			fmt.Fprintf(stdout, "  %s  size=%d bytes\n", item.DatabaseID, item.Length)
		}
		fmt.Fprintln(stdout, "\nNothing was changed. Add --confirm to move these buckets to DeletedDatabases.")
		return 0
	}
	for _, item := range items {
		target, moveErr := manager.Delete(item.DatabaseID, true)
		if moveErr != nil {
			fmt.Fprintln(stderr, moveErr)
			return 1
		}
		fmt.Fprintf(stdout, "Moved %s to %s\n", item.DatabaseID, target)
	}
	return 0
}

func validateSettings(settings config.Settings) error {
	port, err := settings.Int("Port")
	if err != nil || port < 1 || port > 65535 {
		return errors.New("The listening port must be between 1 and 65535.")
	}
	certificate := strings.TrimSpace(settings.String("CertFile"))
	key := strings.TrimSpace(settings.String("KeyFile"))
	if (certificate == "") != (key == "") {
		return errors.New("Clipman Server HTTPS requires both CertFile and KeyFile. No listener was started.")
	}
	if certificate == "" && !isLocalOrPrivateHost(settings.String("Host")) && !settings.Bool("AllowInsecureRemote") {
		return errors.New("Refusing to start an insecure remote Clipman Server listener.\nUse --cert-file and --key-file for direct HTTPS, run behind a TLS reverse proxy, bind to localhost/private VPN address, or pass --allow-insecure-remote only for deliberate private-network testing.")
	}
	return nil
}

func isLocalOrPrivateHost(host string) bool {
	host = strings.Trim(strings.TrimSpace(strings.ToLower(host)), "[]")
	if host == "localhost" || host == "localhost.localdomain" {
		return true
	}
	address, err := netip.ParseAddr(strings.Split(host, "%")[0])
	if err != nil || address.IsUnspecified() {
		return false
	}
	if address.IsLoopback() || address.IsPrivate() || address.IsLinkLocalUnicast() {
		return true
	}
	if address.Is4() {
		carrier, _ := netip.ParsePrefix("100.64.0.0/10")
		return carrier.Contains(address)
	}
	return false
}

func absolutePath(path string) (string, error) {
	if strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"+string(filepath.Separator)))
	}
	return filepath.Abs(path)
}

func printUsage(out io.Writer) {
	fmt.Fprintln(out, "Usage: clipman-server [options]")
}

type runtimeStats struct {
	started       time.Time
	mu            sync.Mutex
	methods       map[string]int64
	status        map[string]int64
	clients       map[string]int64
	request       atomic.Int64
	health        atomic.Int64
	uploads       atomic.Int64
	downloads     atomic.Int64
	polls         atomic.Int64
	conflicts     atomic.Int64
	bytesReceived atomic.Int64
	bytesSent     atomic.Int64
}

func newRuntimeStats() *runtimeStats {
	return &runtimeStats{started: time.Now(), methods: map[string]int64{}, status: map[string]int64{}, clients: map[string]int64{}}
}

func (s *runtimeStats) record(request *http.Request, status int, health bool, received, sent int64) {
	s.request.Add(1)
	if health {
		s.health.Add(1)
	}
	if strings.HasPrefix(request.URL.Path, "/api/v1/database/") {
		switch request.Method {
		case http.MethodPut:
			s.uploads.Add(1)
		case http.MethodGet:
			s.downloads.Add(1)
		case http.MethodHead:
			s.polls.Add(1)
		}
	}
	if status == http.StatusConflict || status == http.StatusPreconditionFailed {
		s.conflicts.Add(1)
	}
	s.bytesReceived.Add(max(0, received))
	s.bytesSent.Add(max(0, sent))
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	s.mu.Lock()
	s.methods[request.Method]++
	s.status[strconv.Itoa(status)]++
	s.clients[host]++
	s.mu.Unlock()
}

func (s *runtimeStats) summary() map[string]any {
	s.mu.Lock()
	methods := make(map[string]int64, len(s.methods))
	statuses := make(map[string]int64, len(s.status))
	for key, value := range s.methods {
		methods[key] = value
	}
	for key, value := range s.status {
		statuses[key] = value
	}
	uniqueClients := len(s.clients)
	s.mu.Unlock()
	return map[string]any{
		"StartedUnixMs":     s.started.UnixMilli(),
		"UptimeSeconds":     int64(time.Since(s.started) / time.Second),
		"Requests":          s.request.Load(),
		"DatabaseUploads":   s.uploads.Load(),
		"DatabaseDownloads": s.downloads.Load(),
		"DatabasePolls":     s.polls.Load(),
		"HealthChecks":      s.health.Load(),
		"Conflicts":         s.conflicts.Load(),
		"BytesReceived":     s.bytesReceived.Load(),
		"BytesSent":         s.bytesSent.Load(),
		"UniqueClients":     uniqueClients,
		"Methods":           methods,
		"StatusCodes":       statuses,
	}
}

func runHTTP(settings config.Settings, configPath, version string, stdout, stderr io.Writer) int {
	maxBytes, _ := settings.Int("MaxDatabaseBytes")
	backupMinutes, _ := settings.Int("BackupIntervalMinutes")
	retentionHours, _ := settings.Int("BackupRetentionHours")
	maxBackups, _ := settings.Int("MaxBackups")
	store, err := blobstore.New(blobstore.Options{
		Root: filepath.Dir(settings.String("DatabasePath")), MaxDatabaseBytes: int64(maxBytes),
		CreateBackupBeforeWrite: settings.Bool("CreateBackupBeforeEveryUpload"),
		BackupInterval:          time.Duration(backupMinutes) * time.Minute,
		BackupRetention:         time.Duration(retentionHours) * time.Hour, MaxBackups: maxBackups,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	port, _ := settings.Int("Port")
	address := net.JoinHostPort(strings.Trim(settings.String("Host"), "[]"), strconv.Itoa(port))
	listener, err := net.Listen("tcp", address)
	if err != nil {
		message := fmt.Sprintf("Clipman Server could not open %s:%d: %v. The port may already be in use or reserved by the operating system. Choose another listening port, then update the address used by Clipman clients.", settings.String("Host"), port, err)
		fmt.Fprintln(stderr, message)
		return bindErrorExitCode
	}
	defer listener.Close()
	stats := newRuntimeStats()
	handler := newHandler(settings, configPath, version, stats, store)
	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	// Listener startup and shutdown remain below.
	scheme := "http"
	if settings.String("CertFile") != "" {
		scheme = "https"
	}
	listenHost := settings.String("Host")
	if strings.Contains(listenHost, ":") && !strings.HasPrefix(listenHost, "[") {
		listenHost = "[" + listenHost + "]"
	}
	fmt.Fprintf(stdout, "Clipman Server %s listening on %s://%s:%d/\n", version, scheme, listenHost, port)
	fmt.Fprintln(stdout, "Use --show-token to print the bearer token for client setup.")
	logger := log.New(stderr, "", log.LstdFlags)
	logger.Printf("Clipman Server %s listening on %s", version, address)
	logger.Printf("Settings: %s", configPath)

	serveDone := make(chan error, 1)
	go func() {
		if scheme == "https" {
			serveDone <- server.ServeTLS(listener, settings.String("CertFile"), settings.String("KeyFile"))
		} else {
			serveDone <- server.Serve(listener)
		}
	}()
	signalContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case serveErr := <-serveDone:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			fmt.Fprintln(stderr, serveErr)
			return 1
		}
	case <-signalContext.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err = server.Shutdown(shutdownContext); err != nil {
			fmt.Fprintf(stderr, "Clipman Server shutdown failed: %v\n", err)
			return 1
		}
		<-serveDone
	}
	logger.Printf("Clipman Server runtime summary (shutdown): requests=%d health_checks=%d", stats.request.Load(), stats.health.Load())
	return 0
}

func newHandler(settings config.Settings, configPath, version string, stats *runtimeStats, store *blobstore.Store) http.Handler {
	maxBytes, _ := settings.Int("MaxDatabaseBytes")
	setupManager := &onboarding.Manager{ConfigPath: configPath}
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		path := strings.TrimRight(request.URL.Path, "/")
		if path == "" {
			path = "/"
		}
		if request.Method == http.MethodGet && path == "/api/v1/health" {
			payload := healthPayload(settings, version, stats)
			data, marshalErr := config.Marshal(payload)
			if marshalErr != nil {
				http.Error(writer, "Internal server error", http.StatusInternalServerError)
				stats.record(request, http.StatusInternalServerError, true, 0, 0)
				return
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.Header().Set("Content-Length", strconv.Itoa(len(data)))
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write(data)
			stats.record(request, http.StatusOK, true, 0, int64(len(data)))
			return
		}
		if strings.HasPrefix(path, "/setup/") {
			handleSetup(writer, request, path, setupManager, settings, stats)
			return
		}
		if !authorized(request, settings.String("AuthToken")) {
			writeText(writer, http.StatusUnauthorized, "Unauthorized")
			stats.record(request, http.StatusUnauthorized, false, 0, int64(len("Unauthorized")))
			return
		}
		prefix := "/api/v1/database/"
		escaped := strings.ToLower(request.URL.EscapedPath())
		if strings.HasPrefix(path, prefix) && !strings.Contains(escaped, "%2f") && !strings.Contains(escaped, "%5c") {
			id := strings.TrimPrefix(path, prefix)
			if blobstore.ValidDatabaseID(id) {
				handleDatabase(writer, request, store, id, int64(maxBytes), settings, version, stats)
				return
			}
		}
		writeText(writer, http.StatusNotFound, "Not found")
		stats.record(request, http.StatusNotFound, false, 0, int64(len("Not found")))
	})
}

func handleSetup(w http.ResponseWriter, r *http.Request, path string, manager *onboarding.Manager, settings config.Settings, stats *runtimeStats) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	download := len(parts) == 3 && parts[2] == "connection.clpconf"
	if len(parts) < 2 || len(parts) > 3 || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		data := []byte("This temporary Clipman setup link is unavailable.\n")
		writeSetup(w, r, http.StatusNotFound, data, "text/plain; charset=utf-8", "")
		stats.record(r, http.StatusNotFound, false, 0, setupSent(r, data))
		return
	}
	state, ok := manager.Lookup(parts[1], download && r.Method == http.MethodGet)
	if !ok {
		data := []byte("This temporary Clipman setup link is unavailable.\n")
		writeSetup(w, r, http.StatusNotFound, data, "text/plain; charset=utf-8", "")
		stats.record(r, http.StatusNotFound, false, 0, setupSent(r, data))
		return
	}
	var sent int64
	if download {
		data, err := onboarding.ConnectionBytes(settings)
		if err != nil {
			writeSetup(w, r, 500, []byte("Internal server error"), "text/plain; charset=utf-8", "")
			stats.record(r, 500, false, 0, setupSent(r, []byte("Internal server error")))
			return
		}
		writeSetup(w, r, 200, data, "application/x-clipman-server-connection", `attachment; filename="clipman-server-connection.clpconf"`)
		sent = setupSent(r, data)
	} else {
		data := onboarding.SetupPage(parts[1], state, r.UserAgent())
		writeSetup(w, r, 200, data, "text/html; charset=utf-8", "")
		sent = setupSent(r, data)
	}
	stats.record(r, 200, false, 0, sent)
}
func setupSent(r *http.Request, data []byte) int64 {
	if r.Method == http.MethodHead {
		return 0
	}
	return int64(len(data))
}
func writeSetup(w http.ResponseWriter, r *http.Request, status int, data []byte, contentType, disposition string) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("Cache-Control", "no-store, max-age=0")
	h.Set("Pragma", "no-cache")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	if disposition != "" {
		h.Set("Content-Disposition", disposition)
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}

func handleDatabase(w http.ResponseWriter, r *http.Request, store *blobstore.Store, id string, maxBytes int64, settings config.Settings, version string, stats *runtimeStats) {
	countedWriter := &countingResponseWriter{ResponseWriter: w}
	w = countedWriter
	setRevision := func(info blobstore.Info) {
		w.Header().Set("ETag", `"`+info.Revision+`"`)
		w.Header().Set("X-Clipman-Revision", info.Revision)
	}
	status := http.StatusOK
	var received int64
	switch r.Method {
	case http.MethodHead:
		info, err := store.Head(id)
		if errors.Is(err, blobstore.ErrNotFound) {
			status = http.StatusNotFound
			w.WriteHeader(status)
			break
		}
		if err != nil {
			status = http.StatusInternalServerError
			writeText(w, status, "Internal server error")
			break
		}
		setRevision(info)
		w.Header().Set("Content-Length", strconv.FormatInt(info.Length, 10))
		w.WriteHeader(status)
	case http.MethodGet:
		_, _, err := store.Get(r.Context(), id, func(info blobstore.Info) error {
			setRevision(info)
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Length", strconv.FormatInt(info.Length, 10))
			w.WriteHeader(status)
			return nil
		}, w)
		if errors.Is(err, blobstore.ErrNotFound) {
			status = http.StatusNotFound
			writeText(w, status, "Database not found")
		} else if err != nil {
			status = http.StatusInternalServerError
		}
	case http.MethodPut:
		if r.ContentLength < 0 {
			status = http.StatusBadRequest
			writeText(w, status, "A valid Content-Length header is required")
			break
		}
		if r.ContentLength > maxBytes {
			status = http.StatusRequestEntityTooLarge
			writeText(w, status, fmt.Sprintf("Database exceeds the configured %d byte limit", maxBytes))
			break
		}
		ifNone, ifMatch := strings.TrimSpace(r.Header.Get("If-None-Match")), strings.Trim(strings.TrimSpace(r.Header.Get("If-Match")), `"`)
		if ifNone != "" && ifNone != "*" {
			status = http.StatusBadRequest
			writeText(w, status, "If-None-Match must be * when creating a database")
			break
		}
		if ifNone != "" && ifMatch != "" {
			status = http.StatusBadRequest
			writeText(w, status, "If-Match and If-None-Match cannot be used together")
			break
		}
		counter := &countingReader{reader: r.Body}
		result, err := store.Put(r.Context(), id, counter, r.ContentLength, blobstore.Conditions{Match: ifMatch, CreateOnly: ifNone == "*"})
		received = counter.count
		var conflict *blobstore.ConflictError
		if errors.As(err, &conflict) {
			status = conflict.Status
			if conflict.Revision != "" {
				w.Header().Set("X-Clipman-Revision", conflict.Revision)
			}
			writeText(w, status, conflict.Message)
			break
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			status = http.StatusBadRequest
			writeText(w, status, "Request body ended before Content-Length bytes were received")
			break
		}
		if err != nil {
			status = http.StatusInternalServerError
			writeText(w, status, "Internal server error")
			break
		}
		setRevision(result.Info)
		data, _ := config.Marshal(healthPayload(settings, version, stats))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(status)
		_, _ = w.Write(data)
	default:
		status = http.StatusNotFound
		writeText(w, status, "Not found")
	}
	stats.record(r, status, false, received, countedWriter.count)
}

type countingReader struct {
	reader io.Reader
	count  int64
}

func (c *countingReader) Read(buffer []byte) (int, error) {
	n, err := c.reader.Read(buffer)
	c.count += int64(n)
	return n, err
}

type countingResponseWriter struct {
	http.ResponseWriter
	count int64
}

func (c *countingResponseWriter) Write(data []byte) (int, error) {
	n, err := c.ResponseWriter.Write(data)
	c.count += int64(n)
	return n, err
}

func healthPayload(settings config.Settings, version string, stats *runtimeStats) map[string]any {
	port, _ := settings.Int("Port")
	scheme := "http"
	if settings.String("CertFile") != "" {
		scheme = "https"
	}
	machine := ""
	if runtime.GOOS != "windows" {
		machine, _ = os.Hostname()
	}
	retention, _ := settings.Int("BackupRetentionHours")
	maxBackups, _ := settings.Int("MaxBackups")
	return map[string]any{
		"Status":                 "ok",
		"Version":                version,
		"Machine":                machine,
		"DatabaseRevision":       "",
		"DatabaseLength":         0,
		"DatabaseModifiedUnixMs": 0,
		"ListenPrefix":           fmt.Sprintf("%s://%s:%d/", scheme, settings.String("Host"), port),
		"TlsEnabled":             scheme == "https",
		"TlsCertificateExpires":  "",
		"BackupRetentionHours":   retention,
		"MaxBackups":             maxBackups,
		"Runtime":                stats.summary(),
	}
}

func authorized(request *http.Request, token string) bool {
	want := "Bearer " + strings.TrimSpace(token)
	got := strings.TrimSpace(request.Header.Get("Authorization"))
	return token != "" && len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func writeText(writer http.ResponseWriter, status int, text string) {
	data := []byte(text)
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Content-Length", strconv.Itoa(len(data)))
	writer.WriteHeader(status)
	_, _ = writer.Write(data)
}
