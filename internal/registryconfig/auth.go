package registryconfig

import (
	"net/url"
	"strings"

	"github.com/distribution/reference"
	registrytypes "github.com/docker/docker/api/types/registry"
)

// EncodedPullAuth returns the X-Registry-Auth value for an image reference, mirroring
// the Docker CLI. An empty string means no credentials were found (public pull / no config).
func EncodedPullAuth(imageRef string) (string, error) {
	named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef))
	if err != nil {
		return "", err
	}
	host := strings.ToLower(reference.Domain(named))
	if host == "" {
		host = "docker.io"
	}

	cfg, err := Load()
	if err != nil {
		return "", err
	}
	entry, ok := cfg.lookup(host)
	if !ok {
		return "", nil
	}

	ac := entry.toAuthConfig(host)
	return registrytypes.EncodeAuthConfig(ac)
}

func (f File) lookup(registryHost string) (AuthEntry, bool) {
	host := NormalizeHost(registryHost)
	if host == "" {
		return AuthEntry{}, false
	}
	candidates := []string{
		host,
		"https://" + host,
		"http://" + host,
	}
	if host == "docker.io" {
		candidates = append(candidates, "https://index.docker.io/v1/", "index.docker.io/v1/")
	}
	for _, key := range candidates {
		if e, ok := f.Auths[key]; ok && HasCredentials(e) {
			return e, true
		}
	}
	for key, e := range f.Auths {
		if NormalizeHost(key) == host && HasCredentials(e) {
			return e, true
		}
	}
	return AuthEntry{}, false
}

func (e AuthEntry) toAuthConfig(server string) registrytypes.AuthConfig {
	ac := registrytypes.AuthConfig{ServerAddress: server}
	if t := strings.TrimSpace(e.IdentityToken); t != "" {
		ac.IdentityToken = t
	}
	if a := strings.TrimSpace(e.Auth); a != "" {
		ac.Auth = a
	} else {
		ac.Username = e.Username
		ac.Password = e.Password
	}
	return ac
}

// NormalizeHost canonicalizes registry host keys from config.json.
func NormalizeHost(in string) string {
	s := strings.TrimSpace(strings.ToLower(in))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			s = strings.ToLower(u.Host)
		}
	}
	s = strings.TrimPrefix(s, "//")
	s = strings.SplitN(s, "/", 2)[0]
	s = strings.TrimSuffix(s, "/")
	if s == "index.docker.io" {
		return "docker.io"
	}
	return s
}
