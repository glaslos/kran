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
	"github.com/glaslos/kran/internal/recreate"
)

// Docker is the subset of the Engine API kran needs (implemented by *docker.Client).
type Docker interface {
	ListRunning(ctx context.Context) ([]types.Container, error)
	Inspect(ctx context.Context, id string) (types.ContainerJSON, error)
	PullImage(ctx context.Context, ref string) error
	ImageInspect(ctx context.Context, ref string) (types.ImageInspect, error)
	Stop(ctx context.Context, id string, timeoutSec *int) error
	Remove(ctx context.Context, id string) error
	Create(ctx context.Context, name string, cfg *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) (string, error)
	Start(ctx context.Context, id string) error
	PruneDanglingImages(ctx context.Context) error
}

// Run polls until ctx is cancelled.
func Run(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker) error {
	next := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-next:
			if err := Tick(ctx, log, cfg, dc); err != nil {
				log.Error("tick failed", "err", err)
			}
			next = time.After(cfg.Interval)
		}
	}
}

// Tick performs one scan of running containers.
func Tick(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker) error {
	list, err := dc.ListRunning(ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := processContainer(ctx, log, cfg, dc, c.ID); err != nil {
			log.Warn("container skipped or failed", "id", c.ID, "err", err)
		}
	}
	return nil
}

func processContainer(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, id string) error {
	in, err := dc.Inspect(ctx, id)
	if err != nil {
		return err
	}
	if !Managed(in, cfg) {
		return nil
	}

	name := strings.TrimPrefix(in.Name, "/")
	imageRef := strings.TrimSpace(in.Config.Image)
	if imageRef == "" {
		return nil
	}

	oldID := normalizeImageID(in.Image)

	if err := dc.PullImage(ctx, imageRef); err != nil {
		return err
	}
	newImg, err := dc.ImageInspect(ctx, imageRef)
	if err != nil {
		return err
	}
	newID := normalizeImageID(newImg.ID)
	if oldID == newID {
		log.Debug("image up to date", "container", name, "image", imageRef)
		return nil
	}

	log.Info("new image available", "container", name, "image", imageRef)

	if cfg.DryRun {
		log.Info("dry-run: would recreate container", "container", name)
		return nil
	}

	params, err := recreate.FromInspect(in, imageRef)
	if err != nil {
		return err
	}

	sec := int(cfg.StopTimeout.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}

	if err := dc.Stop(ctx, id, &sec); err != nil {
		return err
	}
	if err := dc.Remove(ctx, id); err != nil {
		return err
	}

	newCID, err := dc.Create(ctx, params.Name, params.Config, params.HostConfig, params.NetworkingConfig)
	if err != nil {
		return err
	}
	if err := dc.Start(ctx, newCID); err != nil {
		return err
	}

	log.Info("recreated container", "container", params.Name, "new_id", newCID)

	if cfg.Cleanup {
		if err := dc.PruneDanglingImages(ctx); err != nil {
			log.Warn("image prune failed", "err", err)
		}
	}
	return nil
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
