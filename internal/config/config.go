package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration
type Config struct {
	Port          string
	DatabaseURL   string
	APIToken      string
	TMDBAPIKey    string
	LogLevel      string
	SecureCookies bool
	// TrustedProxies are the peers whose X-Forwarded-For header is trusted.
	TrustedProxies []netip.Prefix
	// SessionEpoch must match the epoch in a session cookie; raising it signs
	// out every browser.
	SessionEpoch int64
}

// Load reads configuration from environment variables.
// Supports _FILE suffix pattern for reading secrets from files (Docker Swarm style).
// Also supports fallback to default file paths in /run/secrets/ for Docker Swarm secrets.
func Load() (*Config, error) {
	var err error
	cfg := &Config{}

	if cfg.Port, err = getEnv("PORT", "4600"); err != nil {
		return nil, err
	}
	if cfg.DatabaseURL, err = getEnvOrFile("DATABASE_URL", "/run/secrets/dejaview_database_url"); err != nil {
		return nil, err
	}
	if cfg.APIToken, err = getEnvOrFile("API_TOKEN", "/run/secrets/dejaview_api_token"); err != nil {
		return nil, err
	}
	if cfg.TMDBAPIKey, err = getEnvOrFile("TMDB_API_KEY", "/run/secrets/dejaview_tmdb_api_key"); err != nil {
		return nil, err
	}
	if cfg.LogLevel, err = getEnv("LOG_LEVEL", "info"); err != nil {
		return nil, err
	}

	// Secure cookies enabled by default (production), set SECURE_COOKIES=false for local dev
	secureCookiesStr, err := getEnv("SECURE_COOKIES", "true")
	if err != nil {
		return nil, err
	}
	cfg.SecureCookies = secureCookiesStr != "false"

	trustedProxies, err := getEnv("TRUSTED_PROXY_CIDRS", "")
	if err != nil {
		return nil, err
	}
	if cfg.TrustedProxies, err = parseTrustedProxies(trustedProxies); err != nil {
		return nil, err
	}

	sessionEpoch, err := getEnv("SESSION_EPOCH", "0")
	if err != nil {
		return nil, err
	}
	if cfg.SessionEpoch, err = strconv.ParseInt(strings.TrimSpace(sessionEpoch), 10, 64); err != nil || cfg.SessionEpoch < 0 {
		return nil, fmt.Errorf("SESSION_EPOCH must be a non-negative integer, got %q", sessionEpoch)
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.APIToken == "" {
		return nil, fmt.Errorf("API_TOKEN is required")
	}
	if cfg.TMDBAPIKey == "" {
		return nil, fmt.Errorf("TMDB_API_KEY is required")
	}

	return cfg, nil
}

// parseTrustedProxies reads a comma-separated list of CIDRs or single IPs.
func parseTrustedProxies(value string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if prefix, err := netip.ParsePrefix(entry); err == nil {
			// Client addresses are unmapped before matching, so rebase
			// IPv4-mapped IPv6 prefixes onto IPv4.
			if prefix.Addr().Is4In6() {
				if prefix.Bits() < 96 {
					return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS IPv4-mapped prefix %q must be /96 or longer", entry)
				}
				prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			}
			prefixes = append(prefixes, prefix.Masked())
			continue
		}
		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("TRUSTED_PROXY_CIDRS contains invalid CIDR or IP %q", entry)
		}
		addr = addr.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return prefixes, nil
}

// getEnv checks for FOO_FILE env var first, reads from file if exists,
// otherwise falls back to FOO env var, then to the default value.
// Returns an error if _FILE is set but the file cannot be read.
func getEnv(key, defaultVal string) (string, error) {
	// Check for _FILE variant first (Docker Swarm secrets pattern)
	if filePath := os.Getenv(key + "_FILE"); filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("failed to read %s_FILE (%s): %w", key, filePath, err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	if val := os.Getenv(key); val != "" {
		return val, nil
	}

	return defaultVal, nil
}

// getEnvOrFile checks for the environment variable, then _FILE variant, then falls back to a default file path.
// This supports Docker Swarm secrets which are mounted at /run/secrets/.
// Returns an error only if _FILE is explicitly set but the file cannot be read.
// Returns empty string (no error) if the default path doesn't exist, allowing validation to catch missing required values.
func getEnvOrFile(key, defaultPath string) (string, error) {
	// First check for direct environment variable
	if value := os.Getenv(key); value != "" {
		return value, nil
	}

	// Check for _FILE environment variable
	fileKey := key + "_FILE"
	if path := os.Getenv(fileKey); path != "" {
		return readSecret(path, fileKey)
	}

	// Fall back to default path if provided
	if defaultPath != "" {
		return readSecret(defaultPath, key)
	}

	return "", nil
}

// readSecret reads a secret from the given file path.
// Returns empty string (no error) if the file doesn't exist, allowing validation to catch missing required values.
// Returns an error if the file exists but cannot be read, or if it's empty.
func readSecret(path, name string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("config: reading %s (%s): %w", name, path, err)
	}

	value := strings.TrimSpace(string(contents))
	if value == "" {
		return "", fmt.Errorf("config: %s (%s) is empty", name, path)
	}
	return value, nil
}


