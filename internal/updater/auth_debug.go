package updater

import (
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/distribution/reference"
	"github.com/glaslos/kran/internal/debugagent"
	"github.com/glaslos/kran/internal/linkgroup"
)

func logAuthDebugInfo(log *slog.Logger, managed []linkgroup.Member) {
	privateRegs := monitoredPrivateRegistries(managed)
	if len(privateRegs) == 0 {
		return
	}
	log.Info("detected monitored containers using non-default registries", "registries", strings.Join(privateRegs, ", "))

	info, err := readDockerAuthInfo()
	if err != nil {
		log.Warn("unable to parse docker auth config while monitoring private registries",
			"config", info.configPath,
			"registries", strings.Join(privateRegs, ", "),
			"err", err)
		// #region agent log
		debugagent.Log("H3", "auth_debug.go:logAuthDebugInfo", "auth config read failed", map[string]any{
			"privateRegs": privateRegs, "configPath": info.configPath, "err": err.Error(),
		})
		// #endregion
		return
	}
	// #region agent log
	logAuthAgentNDJSON(privateRegs, info, "after_read")
	// #endregion
	if !info.hasAnyCredentials {
		log.Warn("no docker registry credentials configured while monitoring private registries",
			"config", info.configPath,
			"registries", strings.Join(privateRegs, ", "))
		return
	}

	var missing []string
	for _, reg := range privateRegs {
		if _, ok := info.authHosts[reg]; !ok {
			missing = append(missing, reg)
		}
	}
	if len(missing) > 0 {
		log.Warn("missing explicit auth entries for monitored private registries",
			"config", info.configPath,
			"missing_registries", strings.Join(missing, ", "),
			"configured_registries", strings.Join(sortedKeys(info.authHosts), ", "))
		return
	}

	log.Debug("found explicit auth entries for monitored private registries",
		"config", info.configPath,
		"registries", strings.Join(privateRegs, ", "))
}

func logAuthAgentNDJSON(privateRegs []string, info dockerAuthInfo, stage string) {
	modes := make(map[string]string, len(privateRegs))
	for _, reg := range privateRegs {
		modes[reg] = authModeForRegistry(reg, info)
	}
	debugagent.Log("H1-H4", "auth_debug.go:logAuthAgentNDJSON", "docker auth layout vs private registries", map[string]any{
		"stage":                   stage,
		"privateRegs":             privateRegs,
		"configPath":              info.configPath,
		"credsStoreSet":           info.credsStoreSet,
		"credHelperRegistries":    sortedKeys(info.credHelperHosts),
		"inlineAuthRegistries":    sortedKeys(info.inlineCredHosts),
		"authModeByRegistry":      modes,
		"configFileExists":        configFileExists(info.configPath),
		"hasAnyCredentialsParsed": info.hasAnyCredentials,
		"authHostKeys":            sortedKeys(info.authHosts),
	})
}

func monitoredPrivateRegistries(managed []linkgroup.Member) []string {
	set := map[string]struct{}{}
	for _, m := range managed {
		_, imageRef, _, ok := containerImageState(m.Inspect)
		if !ok {
			continue
		}
		host, private := privateRegistryHost(imageRef)
		if !private {
			continue
		}
		set[host] = struct{}{}
	}
	return sortedKeys(set)
}

func privateRegistryHost(imageRef string) (string, bool) {
	named, err := reference.ParseNormalizedNamed(strings.TrimSpace(imageRef))
	if err != nil {
		return "", false
	}
	host := strings.ToLower(reference.Domain(named))
	switch host {
	case "", "docker.io", "index.docker.io":
		return "", false
	default:
		return host, true
	}
}

type dockerAuthInfo struct {
	configPath        string
	hasAnyCredentials bool
	authHosts         map[string]struct{}
	credsStoreSet     bool
	credHelperHosts   map[string]struct{}
	inlineCredHosts   map[string]struct{}
}

type dockerConfigFile struct {
	Auths map[string]struct {
		Auth          string `json:"auth"`
		IdentityToken string `json:"identitytoken"`
		Username      string `json:"username"`
		Password      string `json:"password"`
	} `json:"auths"`
	CredsStore  string            `json:"credsStore"`
	CredHelpers map[string]string `json:"credHelpers"`
}

func readDockerAuthInfo() (dockerAuthInfo, error) {
	path := dockerConfigPath()
	info := dockerAuthInfo{
		configPath:      path,
		authHosts:       map[string]struct{}{},
		credHelperHosts: map[string]struct{}{},
		inlineCredHosts: map[string]struct{}{},
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return info, err
	}
	var cfg dockerConfigFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return info, err
	}
	info.credsStoreSet = strings.TrimSpace(cfg.CredsStore) != ""
	if info.credsStoreSet || len(cfg.CredHelpers) > 0 {
		info.hasAnyCredentials = true
	}
	for host, a := range cfg.Auths {
		if h := normalizeRegistryHost(host); h != "" {
			info.authHosts[h] = struct{}{}
		}
		if strings.TrimSpace(a.Auth) != "" ||
			strings.TrimSpace(a.IdentityToken) != "" ||
			strings.TrimSpace(a.Username) != "" ||
			strings.TrimSpace(a.Password) != "" {
			info.hasAnyCredentials = true
			if h := normalizeRegistryHost(host); h != "" {
				info.inlineCredHosts[h] = struct{}{}
			}
		}
	}
	for host := range cfg.CredHelpers {
		if h := normalizeRegistryHost(host); h != "" {
			info.authHosts[h] = struct{}{}
			info.credHelperHosts[h] = struct{}{}
		}
	}
	return info, nil
}

func authModeForRegistry(reg string, info dockerAuthInfo) string {
	if _, ok := info.inlineCredHosts[reg]; ok {
		return "inline"
	}
	if _, ok := info.credHelperHosts[reg]; ok {
		return "credhelper"
	}
	if info.credsStoreSet {
		return "credsstore_fallback"
	}
	return "none"
}

func configFileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func dockerConfigPath() string {
	if d := strings.TrimSpace(os.Getenv("DOCKER_CONFIG")); d != "" {
		return filepath.Join(d, "config.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		home = "/root"
	}
	return filepath.Join(home, ".docker", "config.json")
}

func normalizeRegistryHost(in string) string {
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

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
