package updater

import (
	"log/slog"
	"sort"
	"strings"

	"github.com/distribution/reference"
	"github.com/glaslos/kran/internal/linkgroup"
	"github.com/glaslos/kran/internal/registryconfig"
)

func logAuthDebugInfo(log *slog.Logger, managed []linkgroup.Member) {
	privateRegs := monitoredPrivateRegistries(managed)
	if len(privateRegs) == 0 {
		return
	}
	log.Info("detected monitored containers using non-default registries", "registries", strings.Join(privateRegs, ", "))

	sum, err := registryconfig.Summarize()
	if err != nil {
		log.Warn("unable to parse docker auth config while monitoring private registries",
			"config", sum.ConfigPath,
			"registries", strings.Join(privateRegs, ", "),
			"err", err)
		return
	}
	if !sum.HasAnyCredentials {
		log.Warn("no docker registry credentials configured while monitoring private registries",
			"config", sum.ConfigPath,
			"registries", strings.Join(privateRegs, ", "))
		return
	}

	var missing []string
	for _, reg := range privateRegs {
		if _, ok := sum.AuthHosts[reg]; !ok {
			missing = append(missing, reg)
		}
	}
	if len(missing) > 0 {
		log.Warn("missing explicit auth entries for monitored private registries",
			"config", sum.ConfigPath,
			"missing_registries", strings.Join(missing, ", "),
			"configured_registries", strings.Join(sortedKeys(sum.AuthHosts), ", "))
		return
	}

	log.Debug("found explicit auth entries for monitored private registries",
		"config", sum.ConfigPath,
		"registries", strings.Join(privateRegs, ", "))
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

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
