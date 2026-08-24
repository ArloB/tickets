package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config is the server's runtime configuration, resolved by Load from
// (lowest to highest priority) built-in defaults, an OS-appropriate
// config file, TICKETS_* environment variables, and command-line flags
// (product spec §7.3).
type Config struct {
	DataDir string
	Host    string
	Port    string

	// AnonymousRead allows unauthenticated read-only access (product
	// spec §4.2). Load computes it from Host (enabled only when Host is
	// loopback) unless something explicitly overrides it — see
	// resolveAnonymousRead.
	AnonymousRead bool

	// LogFormat is "console" (human-readable, the default) or "json"
	// (product spec §13). Load rejects any other value.
	LogFormat string

	// ShutdownTimeout bounds how long graceful shutdown waits for
	// in-flight requests to finish (product spec §11).
	ShutdownTimeout time.Duration

	// MaxUploadBytes is the per-version attachment upload size limit
	// (ADR 0007), configurable per that ADR's "25 MiB default,
	// configurable" line.
	MaxUploadBytes int64
}

func (c Config) Addr() string { return c.Host + ":" + c.Port }

// fileConfig mirrors Config with every field optional. json.Unmarshal
// only touches fields actually present in the file, so an absent key
// leaves the corresponding pointer nil rather than a present-but-zero
// value (e.g. "port": "") silently overriding a value already resolved
// from an environment variable or flag default.
type fileConfig struct {
	DataDir         *string `json:"data_dir"`
	Host            *string `json:"host"`
	Port            *string `json:"port"`
	AnonymousRead   *bool   `json:"anonymous_read"`
	LogFormat       *string `json:"log_format"`
	ShutdownTimeout *string `json:"shutdown_timeout"`
	MaxUploadBytes  *int64  `json:"max_upload_bytes"`
}

// overrides is what both the config file and the environment layer
// produce: a set of settings that may or may not have an opinion.
// resolvedDefaults.apply merges only the non-nil fields, so an unset
// setting at one layer falls through to whatever the layer below it
// resolved, rather than being clobbered by a zero value.
type overrides struct {
	dataDir         *string
	host            *string
	port            *string
	anonymousRead   *bool
	logFormat       *string
	shutdownTimeout *time.Duration
	maxUploadBytes  *int64
}

type resolvedDefaults struct {
	dataDir        string
	host           string
	port           string
	anonymousRead  *bool // nil until the file or environment layer sets it explicitly
	logFormat      string
	shutdownTime   time.Duration
	maxUploadBytes int64
}

func (r *resolvedDefaults) apply(o overrides) {
	if o.dataDir != nil {
		r.dataDir = *o.dataDir
	}
	if o.host != nil {
		r.host = *o.host
	}
	if o.port != nil {
		r.port = *o.port
	}
	if o.anonymousRead != nil {
		r.anonymousRead = o.anonymousRead
	}
	if o.logFormat != nil {
		r.logFormat = *o.logFormat
	}
	if o.shutdownTimeout != nil {
		r.shutdownTime = *o.shutdownTimeout
	}
	if o.maxUploadBytes != nil {
		r.maxUploadBytes = *o.maxUploadBytes
	}
}

// Load resolves Config from, in increasing priority: built-in
// defaults, the config file (see configFilePath), TICKETS_*
// environment variables, and command-line flags. There is
// deliberately no --config flag: picking the file's own location from
// a flag would need a second, earlier parse pass just to find it
// before the real one runs, for a setting (a JSON file's own path)
// operators can already redirect with TICKETS_CONFIG_FILE. Load never
// prompts and never reads stdin (§7.3's non-interactive requirement).
func Load(args []string) (Config, error) {
	resolved := resolvedDefaults{
		dataDir:        defaultDataDir(),
		host:           "127.0.0.1",
		port:           "8080",
		logFormat:      "console",
		shutdownTime:   10 * time.Second,
		maxUploadBytes: 25 << 20,
	}

	fc, err := loadFileConfig(configFilePath())
	if err != nil {
		return Config{}, err
	}
	resolved.apply(fc)

	ec, err := loadEnvConfig()
	if err != nil {
		return Config{}, err
	}
	resolved.apply(ec)

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	dataDir := fs.String("data-dir", resolved.dataDir, "directory for the SQLite database and managed file storage")
	host := fs.String("host", resolved.host, "bind address; anything non-loopback prints a warning (product spec §10)")
	port := fs.String("port", resolved.port, "port to listen on")
	logFormat := fs.String("log-format", resolved.logFormat, `log output format: "console" or "json" (product spec §13)`)
	shutdownTimeout := fs.Duration("shutdown-timeout", resolved.shutdownTime, "how long graceful shutdown waits for in-flight requests to finish")
	maxUploadBytes := fs.Int64("max-upload-bytes", resolved.maxUploadBytes, "per-version attachment upload size limit in bytes (ADR 0007)")
	anonymousReadDefault := false
	if resolved.anonymousRead != nil {
		anonymousReadDefault = *resolved.anonymousRead
	}
	anonymousRead := fs.Bool("anonymous-read", anonymousReadDefault,
		"allow anonymous read-only access; defaults to enabled only when --host is loopback (product spec §4.2)")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	if *logFormat != "console" && *logFormat != "json" {
		return Config{}, fmt.Errorf(`config: --log-format must be "console" or "json", got %q`, *logFormat)
	}

	cfg := Config{
		DataDir:         *dataDir,
		Host:            *host,
		Port:            *port,
		LogFormat:       *logFormat,
		ShutdownTimeout: *shutdownTimeout,
		MaxUploadBytes:  *maxUploadBytes,
	}
	cfg.AnonymousRead = resolveAnonymousRead(fs, *anonymousRead, resolved.anonymousRead, cfg.Host)

	warnOnInsecureDefaults(cfg)
	return cfg, nil
}

// resolveAnonymousRead implements §4.2's "enabled for loopback-only
// personal use by default": the flag/env/file layers above already
// resolved *anonymousRead to the right value whenever any of them had
// an opinion (fileOrEnvValue != nil, or the flag was passed explicitly
// on the command line) — in that case it's used as-is. Only when
// nothing anywhere expressed a preference does the default get
// computed from the final Host instead of hardcoded to false.
func resolveAnonymousRead(fs *flag.FlagSet, flagValue bool, fileOrEnvValue *bool, host string) bool {
	explicit := fileOrEnvValue != nil
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "anonymous-read" {
			explicit = true
		}
	})
	if explicit {
		return flagValue
	}
	return IsLoopback(host)
}

func warnOnInsecureDefaults(cfg Config) {
	if IsLoopback(cfg.Host) {
		return
	}
	fmt.Fprintf(os.Stderr,
		"WARNING: binding to non-loopback address %q. Anonymous/unauthenticated "+
			"requests may be reachable from other hosts. See product spec §10.\n", cfg.Host)
	if cfg.AnonymousRead {
		fmt.Fprintf(os.Stderr,
			"WARNING: anonymous read access is enabled on a non-loopback bind. Any "+
				"host that can reach %s can read every project on this server without "+
				"authenticating. See product spec §4.2/§10.\n", cfg.Addr())
	}
}

// configFilePath is where Load looks for the optional config file,
// unless TICKETS_CONFIG_FILE overrides it.
func configFilePath() string {
	if v := os.Getenv("TICKETS_CONFIG_FILE"); v != "" {
		return v
	}
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "tickets", "config.json")
}

// loadFileConfig reads the JSON config file at path. A missing file is
// not an error - the file is entirely optional, one of three sources
// §7.3 lists, not a requirement. A malformed one is, so a typo in it
// doesn't silently get ignored.
func loadFileConfig(path string) (overrides, error) {
	if path == "" {
		return overrides{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return overrides{}, nil
	}
	if err != nil {
		return overrides{}, fmt.Errorf("config: read %s: %w", path, err)
	}

	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		return overrides{}, fmt.Errorf("config: parse %s: %w", path, err)
	}

	o := overrides{
		dataDir:        fc.DataDir,
		host:           fc.Host,
		port:           fc.Port,
		anonymousRead:  fc.AnonymousRead,
		logFormat:      fc.LogFormat,
		maxUploadBytes: fc.MaxUploadBytes,
	}
	if fc.ShutdownTimeout != nil {
		d, err := time.ParseDuration(*fc.ShutdownTimeout)
		if err != nil {
			return overrides{}, fmt.Errorf("config: %s: shutdown_timeout %q: %w", path, *fc.ShutdownTimeout, err)
		}
		o.shutdownTimeout = &d
	}
	return o, nil
}

// loadEnvConfig reads TICKETS_* environment variables. Each is
// independently optional - unset means "no opinion here", not "use
// the zero value" (docs/contracts equivalent of fileConfig's pointer
// fields, without needing JSON to express "absent").
func loadEnvConfig() (overrides, error) {
	var o overrides
	if v, ok := os.LookupEnv("TICKETS_DATA_DIR"); ok {
		o.dataDir = &v
	}
	if v, ok := os.LookupEnv("TICKETS_HOST"); ok {
		o.host = &v
	}
	if v, ok := os.LookupEnv("TICKETS_PORT"); ok {
		o.port = &v
	}
	if v, ok := os.LookupEnv("TICKETS_LOG_FORMAT"); ok {
		o.logFormat = &v
	}
	if v, ok := os.LookupEnv("TICKETS_ANONYMOUS_READ"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return overrides{}, fmt.Errorf("config: TICKETS_ANONYMOUS_READ=%q: %w", v, err)
		}
		o.anonymousRead = &b
	}
	if v, ok := os.LookupEnv("TICKETS_SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return overrides{}, fmt.Errorf("config: TICKETS_SHUTDOWN_TIMEOUT=%q: %w", v, err)
		}
		o.shutdownTimeout = &d
	}
	if v, ok := os.LookupEnv("TICKETS_MAX_UPLOAD_BYTES"); ok {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return overrides{}, fmt.Errorf("config: TICKETS_MAX_UPLOAD_BYTES=%q: %w", v, err)
		}
		o.maxUploadBytes = &n
	}
	return o, nil
}

// IsLoopback reports whether host is a loopback address. Exported so
// internal/httpapi's bearer-token-over-plain-HTTP warning (product
// spec §10) can reuse the same check this package applies to the
// configured bind address, rather than a second copy of the same three
// string comparisons.
func IsLoopback(host string) bool {
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func defaultDataDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return "tickets-data"
	}
	return filepath.Join(base, "tickets")
}
