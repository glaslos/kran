package docker

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Client wraps the Docker Engine API used by kran.
type Client struct {
	cli *client.Client
}

// New returns a Docker API client using DOCKER_HOST-style addressing.
func New(dockerHost string) (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost(dockerHost),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &Client{cli: cli}, nil
}

// Ping verifies connectivity to the daemon.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// Close releases the client.
func (c *Client) Close() error {
	return c.cli.Close()
}

// ListRunning returns running containers.
func (c *Client) ListRunning(ctx context.Context) ([]types.Container, error) {
	list, err := c.cli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return nil, fmt.Errorf("docker: list containers: %w", err)
	}
	return list, nil
}

// Inspect returns full container JSON.
func (c *Client) Inspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	in, err := c.cli.ContainerInspect(ctx, id)
	if err != nil {
		return types.ContainerJSON{}, fmt.Errorf("docker: inspect container %s: %w", id, err)
	}
	return in, nil
}

// ImageInspect returns local image details.
func (c *Client) ImageInspect(ctx context.Context, id string) (types.ImageInspect, error) {
	ins, _, err := c.cli.ImageInspectWithRaw(ctx, id)
	if err != nil {
		return types.ImageInspect{}, fmt.Errorf("docker: inspect image %q: %w", id, err)
	}
	return ins, nil
}

// PullImage pulls an image ref, discarding progress output.
func (c *Client) PullImage(ctx context.Context, ref string) error {
	r, err := c.cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("docker: pull %q: %w", ref, err)
	}
	_, copyErr := io.Copy(io.Discard, r)
	closeErr := r.Close()
	if copyErr != nil {
		return fmt.Errorf("docker: pull %q: read progress: %w", ref, copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("docker: pull %q: close body: %w", ref, closeErr)
	}
	return nil
}

// Stop stops a container; timeout is rounded to whole seconds (minimum 1 if non-zero).
func (c *Client) Stop(ctx context.Context, id string, timeoutSec *int) error {
	if err := c.cli.ContainerStop(ctx, id, container.StopOptions{Timeout: timeoutSec}); err != nil {
		return fmt.Errorf("docker: stop container %s: %w", id, err)
	}
	return nil
}

// Remove removes a container. When removeVolumes is true, anonymous volumes attached to the container are removed.
func (c *Client) Remove(ctx context.Context, id string, removeVolumes bool) error {
	if err := c.cli.ContainerRemove(ctx, id, container.RemoveOptions{RemoveVolumes: removeVolumes, Force: true}); err != nil {
		return fmt.Errorf("docker: remove container %s: %w", id, err)
	}
	return nil
}

// Create creates a container.
func (c *Client) Create(ctx context.Context, name string, cfg *container.Config, hostConfig *container.HostConfig, networking *network.NetworkingConfig) (string, error) {
	createResp, err := c.cli.ContainerCreate(ctx, cfg, hostConfig, networking, nil, name)
	if err != nil {
		return "", fmt.Errorf("docker: create container %q: %w", name, err)
	}
	return createResp.ID, nil
}

// Start starts a container.
func (c *Client) Start(ctx context.Context, id string) error {
	if err := c.cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
		return fmt.Errorf("docker: start container %s: %w", id, err)
	}
	return nil
}

// PruneDanglingImages removes dangling images after updates (best-effort).
func (c *Client) PruneDanglingImages(ctx context.Context) error {
	report, err := c.cli.ImagesPrune(ctx, filters.Args{})
	if err != nil {
		return fmt.Errorf("docker: prune images: %w", err)
	}
	_ = report
	return nil
}

// Raw exposes the underlying client for advanced operations if needed.
func (c *Client) Raw() *client.Client {
	return c.cli
}

// NormalizeImageRef trims whitespace from image reference strings.
func NormalizeImageRef(s string) string {
	return strings.TrimSpace(s)
}
