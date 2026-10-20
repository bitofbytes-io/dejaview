package config

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://localhost/dejaview")
	t.Setenv("API_TOKEN", "test-token")
	t.Setenv("TMDB_READ_ACCESS_TOKEN", "test-read-access-token")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	t.Setenv("SESSION_EPOCH", "")
	t.Setenv("METRICS_ADDR", "")
}

func TestMetricsAddr(t *testing.T) {
	setRequiredEnv(t)

	for _, tc := range []struct{ value, want string }{
		{"", ":9464"},
		{"  ", ":9464"},
		{"127.0.0.1:9100", "127.0.0.1:9100"},
		{"off", ""},
		{"OFF", ""},
	} {
		t.Setenv("METRICS_ADDR", tc.value)
		cfg, err := Load()
		if err != nil || cfg.MetricsAddr != tc.want {
			t.Fatalf("METRICS_ADDR=%q: addr = %v, %v; want %q", tc.value, cfg, err, tc.want)
		}
	}
}

func TestTrustedProxyCIDRs(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil || len(cfg.TrustedProxies) != 0 {
		t.Fatalf("default trusted proxies = %v, %v; want none", cfg, err)
	}

	prefixes := func(cfg *Config) string {
		var got []string
		for _, prefix := range cfg.TrustedProxies {
			got = append(got, prefix.String())
		}
		return strings.Join(got, ",")
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", " 10.0.1.7/24, 192.0.2.10 ,fd00::/8")
	if cfg, err = Load(); err != nil || prefixes(cfg) != "10.0.1.0/24,192.0.2.10/32,fd00::/8" {
		t.Fatalf("trusted proxies = %v, %v", cfg, err)
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", "::ffff:10.0.1.0/120, ::ffff:192.0.2.10/128, ::ffff:192.0.2.10")
	if cfg, err = Load(); err != nil || prefixes(cfg) != "10.0.1.0/24,192.0.2.10/32,192.0.2.10/32" {
		t.Fatalf("IPv4-mapped trusted proxies = %v, %v", cfg, err)
	}
	if !cfg.TrustedProxies[0].Contains(netip.MustParseAddr("10.0.1.5")) {
		t.Fatal("rebased IPv4-mapped prefix does not match an unmapped IPv4 address")
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", "::ffff:0.0.0.0/95")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "/96 or longer") {
		t.Fatalf("Load error = %v, want IPv4-mapped prefix length error", err)
	}

	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.1.0/24,proxy")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Fatalf("Load error = %v, want TRUSTED_PROXY_CIDRS error", err)
	}
}

func TestSessionEpoch(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil || cfg.SessionEpoch != 0 {
		t.Fatalf("default session epoch = %v, %v; want 0", cfg, err)
	}

	t.Setenv("SESSION_EPOCH", " 3 ")
	if cfg, err = Load(); err != nil || cfg.SessionEpoch != 3 {
		t.Fatalf("session epoch = %v, %v; want 3", cfg, err)
	}

	for _, bad := range []string{"-1", "two", "1.5"} {
		t.Setenv("SESSION_EPOCH", bad)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SESSION_EPOCH") {
			t.Fatalf("SESSION_EPOCH=%q: Load error = %v, want SESSION_EPOCH error", bad, err)
		}
	}
}

func TestLoadDatabaseURL(t *testing.T) {
	// The hook's migration job sets only DATABASE_URL_FILE.
	t.Setenv("DATABASE_URL", "")
	secret := filepath.Join(t.TempDir(), "dejaview_database_url")
	if err := os.WriteFile(secret, []byte("postgres://secret/dejaview\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", secret)
	if got, err := LoadDatabaseURL(); err != nil || got != "postgres://secret/dejaview" {
		t.Fatalf("LoadDatabaseURL from file = %q, %v", got, err)
	}

	t.Setenv("DATABASE_URL", "postgres://env/dejaview")
	if got, err := LoadDatabaseURL(); err != nil || got != "postgres://env/dejaview" {
		t.Fatalf("LoadDatabaseURL from env = %q, %v; want the variable to win", got, err)
	}

	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", filepath.Join(t.TempDir(), "missing"))
	if _, err := LoadDatabaseURL(); err != nil {
		t.Fatalf("LoadDatabaseURL with missing file = %v, want empty value for the caller to reject", err)
	}
}

func TestTMDBReadAccessTokenConfiguration(t *testing.T) {
	setRequiredEnv(t)
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "read-access-token")
	if err := os.WriteFile(tokenFile, []byte("fake-file-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Explicit paths keep these tests isolated from any real default secret.
	t.Setenv("TMDB_READ_ACCESS_TOKEN_FILE", tokenFile)
	t.Setenv("TMDB_READ_ACCESS_TOKEN", "fake-env-token")
	cfg, err := Load()
	if err != nil || cfg.TMDBReadAccessToken != "fake-env-token" {
		t.Fatalf("environment precedence failed: %v", err)
	}
	t.Setenv("TMDB_READ_ACCESS_TOKEN", "")
	cfg, err = Load()
	if err != nil || cfg.TMDBReadAccessToken != "fake-file-token" {
		t.Fatalf("file token loading failed: %v", err)
	}
	t.Setenv("TMDB_READ_ACCESS_TOKEN_FILE", filepath.Join(dir, "missing"))
	t.Setenv("TMDB_API_KEY", "fake-legacy-key")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "TMDB_READ_ACCESS_TOKEN is required") {
		t.Fatalf("legacy API key must not satisfy token requirement: %v", err)
	}
	t.Setenv("TMDB_READ_ACCESS_TOKEN_FILE", tokenFile)
	if err := os.WriteFile(tokenFile, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Fatalf("empty token file accepted: %v", err)
	}
	t.Setenv("TMDB_READ_ACCESS_TOKEN_FILE", dir)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "reading TMDB_READ_ACCESS_TOKEN_FILE") {
		t.Fatalf("unreadable token file accepted: %v", err)
	}
	for _, token := range []string{" ", "fake token", "fake\r\nInjected: secret", "faké-token"} {
		t.Setenv("TMDB_READ_ACCESS_TOKEN", token)
		if _, err := Load(); err == nil || (strings.TrimSpace(token) != "" && strings.Contains(err.Error(), token)) {
			t.Fatalf("invalid token accepted or echoed: %v", err)
		}
	}
}

func TestReadAccessTokenDefaultFilePrecedence(t *testing.T) {
	// Exercise the shared loader with fake default/explicit mounts; never read
	// the actual /run/secrets/dejaview_tmdb_read_access_token path in tests.
	dir := t.TempDir()
	defaultFile := filepath.Join(dir, "default-read-token")
	explicitFile := filepath.Join(dir, "explicit-read-token")
	for path, token := range map[string]string{defaultFile: "fake-default", explicitFile: "fake-explicit"} {
		if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ env, file, want string }{
		{"fake-env", explicitFile, "fake-env"},
		{"", explicitFile, "fake-explicit"},
		{"", "", "fake-default"},
		{"", filepath.Join(dir, "missing"), ""},
	} {
		t.Setenv("TMDB_READ_ACCESS_TOKEN", tc.env)
		t.Setenv("TMDB_READ_ACCESS_TOKEN_FILE", tc.file)
		token, err := getEnvOrFile("TMDB_READ_ACCESS_TOKEN", defaultFile)
		if err != nil || token != tc.want {
			t.Fatalf("credential source precedence failed: %v", err)
		}
	}
}
