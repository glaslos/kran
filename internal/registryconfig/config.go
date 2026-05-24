package registryconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AuthEntry is one registry entry from Docker's config.json "auths" map.
type AuthEntry struct {
	Auth          string `json:"auth"`
	IdentityToken string `json:"identitytoken"`
	Username      string `json:"username"`
	Password      string `json:"password"`
}

// File is the subset of Docker's config.json used for registry authentication.
type File struct {
	Path        string
	Auths       map[string]AuthEntry
	CredsStore  string
	CredHelpers map[string]string
}

// ConfigPath returns the Docker client config file path (config.json).
func ConfigPath() string {
	if d := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); d != "" {
		return filepath.Join(d, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "/root"
	}
	return filepath.Join(home, ".docker", "config.json")
}

type dockerConfigJSON struct {
	Auths       map[string]AuthEntry `json:"auths"`
	CredsStore  string               `json:"credsStore"`
	CredHelpers map[string]string    `json:"credHelpers"`
}

// Load reads and parses the Docker config file. A missing file yields an empty File.
func Load() (File, error) {
	path := ConfigPath()
	out := File{
		Path:        path,
		Auths:       map[string]AuthEntry{},
		CredHelpers: map[string]string{},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	var raw dockerConfigJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return out, err
	}
	if raw.Auths != nil {
		out.Auths = raw.Auths
	}
	out.CredsStore = strings.TrimSpace(raw.CredsStore)
	if raw.CredHelpers != nil {
		out.CredHelpers = raw.CredHelpers
	}
	return out, nil
}

// HasCredentials reports whether an auth entry contains usable credentials.
func HasCredentials(a AuthEntry) bool {
	return strings.TrimSpace(a.Auth) != "" ||
		strings.TrimSpace(a.IdentityToken) != "" ||
		strings.TrimSpace(a.Username) != "" ||
		strings.TrimSpace(a.Password) != ""
}

// Summary supports startup diagnostics for private-registry monitoring.
type Summary struct {
	ConfigPath        string
	HasAnyCredentials bool
	AuthHosts         map[string]struct{}
}

// Summarize returns credential layout from the Docker config file.
func Summarize() (Summary, error) {
	cfg, err := Load()
	if err != nil {
		return Summary{ConfigPath: cfg.Path}, err
	}
	sum := Summary{
		ConfigPath: cfg.Path,
		AuthHosts:  map[string]struct{}{},
	}
	if cfg.CredsStore != "" || len(cfg.CredHelpers) > 0 {
		sum.HasAnyCredentials = true
	}
	for host, a := range cfg.Auths {
		if h := NormalizeHost(host); h != "" {
			sum.AuthHosts[h] = struct{}{}
		}
		if HasCredentials(a) {
			sum.HasAnyCredentials = true
		}
	}
	for host := range cfg.CredHelpers {
		if h := NormalizeHost(host); h != "" {
			sum.AuthHosts[h] = struct{}{}
		}
	}
	return sum, nil
}
