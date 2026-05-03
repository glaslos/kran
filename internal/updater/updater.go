package updater

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/glaslos/kran/internal/config"
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

// Run polls until ctx is cancelled.
func Run(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, m *metrics.Metrics) error {
	next := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
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
	managedCount := 0
	for _, c := range list {
		select {
		case <-ctx.Done():
			m.ObserveTick(time.Since(start), len(list), managedCount, ctx.Err())
			return ctx.Err()
		default:
		}
		hintName, hintImage := listContainerHint(c)
		ok, err := processContainer(ctx, log, cfg, dc, m, c.ID)
		if ok {
			managedCount++
		}
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

	if cfg.Cleanup {
		if err := dc.PruneDanglingImages(ctx); err != nil {
			log.Warn("image prune failed", "err", err)
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
