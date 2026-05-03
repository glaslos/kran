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
		hintName, hintImage := listContainerHint(c)
		if err := processContainer(ctx, log, cfg, dc, c.ID); err != nil {
			log.Warn("container skipped or failed",
				"id", c.ID,
				"container", hintName,
				"image", hintImage,
				"err", err)
		}
	}
	return nil
}

func listContainerHint(c types.Container) (name, image string) {
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	return name, c.Image
}

func processContainer(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, id string) error {
	in, err := dc.Inspect(ctx, id)
	if err != nil {
		return err
	}
	if !Managed(in, cfg) {
		return nil
	}

	name, imageRef, oldImageID, ok := containerImageState(in)
	if !ok {
		return nil
	}

	log.Debug("checking container",
		"container", name,
		"image", imageRef,
		"container_id", shortID(id))

	changed, newImageID, err := imageChangedAfterPull(ctx, dc, imageRef, oldImageID)
	if err != nil {
		return err
	}
	if !changed {
		log.Debug("image up to date",
			"container", name,
			"image", imageRef,
			"image_id", shortID(oldImageID))
		return nil
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
		return nil
	}

	return recreateContainer(ctx, log, cfg, dc, id, in, imageRef)
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

func imageChangedAfterPull(ctx context.Context, dc Docker, imageRef, oldImageID string) (changed bool, newImageID string, err error) {
	if err := dc.PullImage(ctx, imageRef); err != nil {
		return false, "", err
	}
	newImg, err := dc.ImageInspect(ctx, imageRef)
	if err != nil {
		return false, "", err
	}
	newID := normalizeImageID(newImg.ID)
	return oldImageID != newID, newID, nil
}

func recreateContainer(ctx context.Context, log *slog.Logger, cfg *config.Config, dc Docker, oldCID string, in types.ContainerJSON, imageRef string) error {
	params, err := recreate.FromInspect(in, imageRef)
	if err != nil {
		return err
	}

	sec := int(cfg.StopTimeout.Round(time.Second) / time.Second)
	if sec < 1 {
		sec = 1
	}

	if err := dc.Stop(ctx, oldCID, &sec); err != nil {
		return err
	}
	if err := dc.Remove(ctx, oldCID, cfg.Cleanup); err != nil {
		return err
	}

	newCID, err := dc.Create(ctx, params.Name, params.Config, params.HostConfig, params.NetworkingConfig)
	if err != nil {
		return err
	}
	if err := dc.Start(ctx, newCID); err != nil {
		return err
	}

	log.Info("recreated container",
		"container", params.Name,
		"old_container_id", shortID(oldCID),
		"new_container_id", shortID(newCID))

	if cfg.NotifyURL != "" {
		body := notify.FormatContainerUpdated(params.Name, imageRef, shortID(oldCID), shortID(newCID))
		if err := notify.Send(cfg.NotifyURL, "kran: container updated", body); err != nil {
			log.Warn("notify failed", "err", err)
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
