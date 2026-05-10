package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/glaslos/kran/internal/config"
	"github.com/glaslos/kran/internal/linkgroup"
	"github.com/glaslos/kran/internal/metrics"
	"github.com/glaslos/kran/internal/notify"
	"github.com/glaslos/kran/internal/recreate"
)

// Docker is the subset of the Engine API kran needs (implemented by *docker.Client).
type Docker interface {
	ListRunning(ctx context.Context) ([]types.Container, error)
	Inspect(ctx context.Context, id string) (types.ContainerJSON, error)
	PullImage(ctx context.Context, ref string) error
	ImageInspect(ctx context.Context, ref string) (types.ImageInspect, error)
	Stop(ctx context.Context, id string, timeoutSec *int) error
	Remove(ctx context.Context, id string, removeVolumes bool) error
	Create(ctx context.Context, name string, cfg *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) (string, error)
	Start(ctx context.Context, id string) error
	PruneDanglingImages(ctx context.Context) error
}

// Run polls until ctx is cancelled. If onDemand is non-nil, receiving from it runs an immediate Tick
// and resets the poll timer (same as completing a scheduled tick).
func Run(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, onDemand <-chan struct{}) error {
	next := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-onDemand:
			if err := Tick(ctx, log, cfg, dc, m); err != nil {
				log.Error("tick failed", "err", err)
			}
			next = time.After(cfg.Interval)
		case <-next:
			if err := Tick(ctx, log, cfg, dc, m); err != nil {
				log.Error("tick failed", "err", err)
			}
			next = time.After(cfg.Interval)
		}
	}
}

// Tick performs one scan of running containers.
func Tick(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics) error {
	start := time.Now()
	list, err := dc.ListRunning(ctx)
	if err != nil {
		m.ObserveTick(time.Since(start), 0, 0, err)
		return err
	}

	var managed []linkgroup.Member
	for _, c := range list {
		select {
		case <-ctx.Done():
			m.ObserveTick(time.Since(start), len(list), len(managed), ctx.Err())
			return ctx.Err()
		default:
		}
		in, err := dc.Inspect(ctx, c.ID)
		if err != nil {
			hintName, hintImage := listContainerHint(c)
			log.Warn("inspect failed", "id", c.ID, "container", hintName, "image", hintImage, "err", err)
			continue
		}
		if !Managed(in, cfg) {
			continue
		}
		managed = append(managed, linkgroup.NewMember(c.ID, in))
	}
	managedCount := len(managed)
	logAuthDebugInfo(log, managed)

	byGroup := linkgroup.ClusterByGroup(managed)
	inLinkGroup := make(map[string]struct{})
	for _, ms := range byGroup {
		for _, mm := range ms {
			inLinkGroup[mm.ID] = struct{}{}
		}
	}

	for g, ms := range byGroup {
		select {
		case <-ctx.Done():
			m.ObserveTick(time.Since(start), len(list), managedCount, ctx.Err())
			return ctx.Err()
		default:
		}
		if err := rolloutLinkGroup(ctx, log, cfg, dc, m, g, ms); err != nil {
			log.Warn("link group skipped or failed", "link_group", g, "err", err)
		}
	}

	for _, c := range list {
		select {
		case <-ctx.Done():
			m.ObserveTick(time.Since(start), len(list), managedCount, ctx.Err())
			return ctx.Err()
		default:
		}
		if _, grouped := inLinkGroup[c.ID]; grouped {
			continue
		}
		hintName, hintImage := listContainerHint(c)
		_, err := processContainer(ctx, log, cfg, dc, m, c.ID)
		if err != nil {
			log.Warn("container skipped or failed",
				"id", c.ID,
				"container", hintName,
				"image", hintImage,
				"err", err)
		}
	}
	m.ObserveTick(time.Since(start), len(list), managedCount, nil)
	return nil
}

func listContainerHint(c types.Container) (name, image string) {
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	return name, c.Image
}

func processContainer(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, id string) (managed bool, err error) {
	in, err := dc.Inspect(ctx, id)
	if err != nil {
		return false, err
	}
	if !Managed(in, cfg) {
		return false, nil
	}
	managed = true

	name, imageRef, oldImageID, ok := containerImageState(in)
	if !ok {
		return managed, nil
	}

	log.Debug("checking container",
		"container", name,
		"image", imageRef,
		"container_id", shortID(id))

	changed, newImageID, err := imageChangedAfterPull(ctx, dc, m, imageRef, oldImageID)
	if err != nil {
		return managed, err
	}
	if !changed {
		log.Debug("image up to date",
			"container", name,
			"image", imageRef,
			"image_id", shortID(oldImageID))
		return managed, nil
	}

	log.Info("new image available",
		"container", name,
		"image", imageRef,
		"old_image_id", shortID(oldImageID),
		"new_image_id", shortID(newImageID))

	if cfg.DryRun {
		log.Info("dry-run: would recreate container",
			"container", name,
			"image", imageRef,
			"old_image_id", shortID(oldImageID),
			"new_image_id", shortID(newImageID))
		m.ObserveUpdate("dry_run", 0)
		return managed, nil
	}

	return managed, recreateContainer(ctx, log, cfg, dc, m, id, in, imageRef)
}

func rolloutLinkGroup(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, group string, members []linkgroup.Member) error {
	if len(members) == 0 {
		return nil
	}

	memberByID := make(map[string]linkgroup.Member, len(members))
	for _, mm := range members {
		memberByID[mm.ID] = mm
	}

	anyChanged := false
	for _, mm := range members {
		_, imageRef, oldImageID, ok := containerImageState(mm.Inspect)
		if !ok {
			continue
		}
		log.Debug("checking link group container",
			"link_group", group,
			"container", mm.Name,
			"image", imageRef,
			"container_id", shortID(mm.ID))
		changed, newImageID, err := imageChangedAfterPull(ctx, dc, m, imageRef, oldImageID)
		if err != nil {
			return err
		}
		if changed {
			anyChanged = true
			log.Info("new image available",
				"link_group", group,
				"container", mm.Name,
				"image", imageRef,
				"old_image_id", shortID(oldImageID),
				"new_image_id", shortID(newImageID))
		}
	}
	if !anyChanged {
		return nil
	}

	orders, err := linkgroup.ComputeOrders(members, func(dep string) {
		log.Warn("kran.depends_on does not match another container in the same link group (ignored)",
			"link_group", group, "depends_on", dep)
	})
	if err != nil {
		return err
	}
	if orders.Ambiguous {
		log.Warn("link group has multiple containers but no kran.depends_on edges among members; using deterministic name order",
			"link_group", group)
	}

	for _, mm := range members {
		_, imageRef, _, ok := containerImageState(mm.Inspect)
		if !ok {
			return fmt.Errorf("container %q: no image in inspect", mm.Name)
		}
		if _, err := recreate.FromInspect(mm.Inspect, imageRef); err != nil {
			return fmt.Errorf("container %q: %w", mm.Name, err)
		}
	}

	if cfg.DryRun {
		for _, mm := range members {
			_, imageRef, _, ok := containerImageState(mm.Inspect)
			if !ok {
				continue
			}
			log.Info("dry-run: would recreate container (link group)",
				"link_group", group,
				"container", mm.Name,
				"image", imageRef)
			m.ObserveUpdate("dry_run", 0)
		}
		return nil
	}

	sec := int(cfg.StopTimeout.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}

	for _, id := range orders.Stop {
		mm := memberByID[id]
		if err := dc.Stop(ctx, id, &sec); err != nil {
			return fmt.Errorf("stop %s: %w", mm.Name, err)
		}
		if err := dc.Remove(ctx, id, cfg.Cleanup); err != nil {
			return fmt.Errorf("remove %s: %w", mm.Name, err)
		}
	}

	for _, id := range orders.Start {
		mm := memberByID[id]
		_, imageRef, _, ok := containerImageState(mm.Inspect)
		if !ok {
			return fmt.Errorf("container %q: no image in inspect", mm.Name)
		}
		if err := createAndStartFromInspect(ctx, log, cfg, dc, m, mm.Inspect, imageRef, mm.ID); err != nil {
			return fmt.Errorf("recreate %s: %w", mm.Name, err)
		}
	}

	log.Info("recreated link group",
		"link_group", group,
		"containers", len(members))

	if cfg.Cleanup {
		if err := dc.PruneDanglingImages(ctx); err != nil {
			log.Warn("image prune failed", "err", err)
		}
	}
	return nil
}

func containerImageState(in types.ContainerJSON) (name, imageRef, oldImageID string, ok bool) {
	if in.Config == nil {
		return "", "", "", false
	}
	name = strings.TrimPrefix(in.Name, "/")
	imageRef = strings.TrimSpace(in.Config.Image)
	if imageRef == "" {
		return "", "", "", false
	}
	oldImageID = normalizeImageID(in.Image)
	return name, imageRef, oldImageID, true
}

func imageChangedAfterPull(ctx context.Context, dc Docker, m *metrics.Metrics, imageRef, oldImageID string) (changed bool, newImageID string, err error) {
	pullStart := time.Now()
	if err := dc.PullImage(ctx, imageRef); err != nil {
		m.ObservePull("failure", time.Since(pullStart))
		return false, "", err
	}
	m.ObservePull("success", time.Since(pullStart))
	newImg, err := dc.ImageInspect(ctx, imageRef)
	if err != nil {
		return false, "", err
	}
	newID := normalizeImageID(newImg.ID)
	return oldImageID != newID, newID, nil
}

func recreateContainer(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, oldCID string, in types.ContainerJSON, imageRef string) error {
	params, err := recreate.FromInspect(in, imageRef)
	if err != nil {
		m.ObserveUpdate("failure", 0)
		return err
	}

	sec := int(cfg.StopTimeout.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}

	recStart := time.Now()
	if err := dc.Stop(ctx, oldCID, &sec); err != nil {
		m.ObserveUpdate("failure", time.Since(recStart))
		return err
	}
	if err := dc.Remove(ctx, oldCID, cfg.Cleanup); err != nil {
		m.ObserveUpdate("failure", time.Since(recStart))
		return err
	}

	if err := createAndStartFromParams(ctx, log, cfg, dc, m, recStart, params, imageRef, oldCID); err != nil {
		return err
	}

	if cfg.Cleanup {
		if err := dc.PruneDanglingImages(ctx); err != nil {
			log.Warn("image prune failed", "err", err)
		}
	}
	return nil
}

func createAndStartFromInspect(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, in types.ContainerJSON, imageRef, oldCID string) error {
	params, err := recreate.FromInspect(in, imageRef)
	if err != nil {
		m.ObserveUpdate("failure", 0)
		return err
	}
	recStart := time.Now()
	return createAndStartFromParams(ctx, log, cfg, dc, m, recStart, params, imageRef, oldCID)
}

func createAndStartFromParams(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics, recStart time.Time, params *recreate.Params, imageRef, oldCID string) error {
	newCID, err := dc.Create(ctx, params.Name, params.Config, params.HostConfig, params.NetworkingConfig)
	if err != nil {
		m.ObserveUpdate("failure", time.Since(recStart))
		return err
	}
	if err := dc.Start(ctx, newCID); err != nil {
		m.ObserveUpdate("failure", time.Since(recStart))
		return err
	}

	m.ObserveUpdate("success", time.Since(recStart))

	log.Info("recreated container",
		"container", params.Name,
		"old_container_id", shortID(oldCID),
		"new_container_id", shortID(newCID))

	if cfg.NotifyURL != "" {
		body := notify.FormatContainerUpdated(params.Name, imageRef, shortID(oldCID), shortID(newCID))
		if err := notify.Send(cfg.NotifyURL, "kran: container updated", body); err != nil {
			log.Warn("notify failed", "err", err)
			m.ObserveNotify("failure")
		} else {
			m.ObserveNotify("success")
		}
	}
	return nil
}

func shortID(id string) string {
	id = normalizeImageID(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func normalizeImageID(id string) string {
	id = strings.TrimSpace(id)
	id = strings.TrimPrefix(id, "sha256:")
	return strings.ToLower(id)
}

// Managed reports whether a container should be considered for updates.
func Managed(in types.ContainerJSON, cfg *config.Config) bool {
	if in.Config == nil {
		return false
	}
	labels := in.Config.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if strings.EqualFold(labels[config.LabelIgnoreKey], "true") {
		return false
	}
	if cfg.LabelEnable {
		v, ok := labels[config.LabelEnableKey]
		if !ok || !strings.EqualFold(v, "true") {
			return false
		}
	}
	if cfg.SelfName != "" {
		n := strings.TrimPrefix(in.Name, "/")
		if n == cfg.SelfName {
			return false
		}
	}
	return true
}

func logAuthDebugInfo(log *slog.Logger, managed []linkgroup.Member) {
	privateRegs := monitoredPrivateRegistries(managed)
	if len(privateRegs) == 0 {
		return
	}
	log.Info("detected monitored containers using non-default registries", "registries", strings.Join(privateRegs, ","))

	info, err := readDockerAuthInfo()
	if err != nil {
		log.Warn("unable to parse docker auth config while monitoring private registries",
			"config", info.configPath,
			"registries", strings.Join(privateRegs, ","),
			"err", err)
		return
	}
	if !info.hasAnyCredentials {
		log.Warn("no docker registry credentials configured while monitoring private registries",
			"config", info.configPath,
			"registries", strings.Join(privateRegs, ","))
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
			"missing_registries", strings.Join(missing, ","),
			"configured_registries", strings.Join(sortedKeys(info.authHosts), ","))
		return
	}

	log.Debug("found explicit auth entries for monitored private registries",
		"config", info.configPath,
		"registries", strings.Join(privateRegs, ","))
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
		configPath: path,
		authHosts:  map[string]struct{}{},
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
	if strings.TrimSpace(cfg.CredsStore) != "" || len(cfg.CredHelpers) > 0 {
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
		}
	}
	for host := range cfg.CredHelpers {
		if h := normalizeRegistryHost(host); h != "" {
			info.authHosts[h] = struct{}{}
		}
	}
	return info, nil
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
