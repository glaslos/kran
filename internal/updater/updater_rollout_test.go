package updater

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/glaslos/kran/internal/config"
	"github.com/glaslos/kran/internal/metrics"
)

// rolloutFake implements Docker for link-group rollout ordering tests.
type rolloutFake struct {
	dbID, appID string
	stopped     []string
	removed     []string
	started     []string
	created     []string

	// Local image digest per ref (ImageInspect); PullImage may update this.
	imgRefDigest map[string]string
	// Image ID stored on each running container (Inspect); unchanged until recreate.
	ctrImage map[string]string
	// When true, PullImage leaves imgRefDigest unchanged (no new image on registry).
	skipDigestBump bool
}

func newRolloutFake() *rolloutFake {
	f := &rolloutFake{
		dbID:  "dbdbdbdbdbdb",
		appID: "appappappappap",
		imgRefDigest: map[string]string{
			"postgres:16":   "sha256:db111",
			"my/app:latest": "sha256:app111",
		},
	}
	f.ctrImage = map[string]string{
		f.dbID:  "sha256:db111",
		f.appID: "sha256:app111",
	}
	return f
}

func (f *rolloutFake) ListRunning(ctx context.Context) ([]types.Container, error) {
	return []types.Container{
		{ID: f.dbID, Names: []string{"/mydb"}},
		{ID: f.appID, Names: []string{"/myapp"}},
	}, nil
}

func (f *rolloutFake) Inspect(ctx context.Context, id string) (types.ContainerJSON, error) {
	hc := &container.HostConfig{NetworkMode: "bridge"}
	ns := &types.NetworkSettings{
		Networks: map[string]*network.EndpointSettings{
			"bridge": {},
		},
	}
	if id == f.dbID {
		return types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{
				Name:       "/mydb",
				Image:      f.ctrImage[f.dbID],
				HostConfig: hc,
			},
			Config: &container.Config{
				Image: "postgres:16",
				Labels: map[string]string{
					config.LabelLinkGroupKey: "stack",
				},
			},
			NetworkSettings: ns,
		}, nil
	}
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name:       "/myapp",
			Image:      f.ctrImage[f.appID],
			HostConfig: hc,
		},
		Config: &container.Config{
			Image: "my/app:latest",
			Labels: map[string]string{
				config.LabelLinkGroupKey:   "stack",
				config.LabelDependsOnKey: "mydb",
			},
		},
		NetworkSettings: ns,
	}, nil
}

func (f *rolloutFake) PullImage(ctx context.Context, ref string) error {
	if !f.skipDigestBump && ref == "my/app:latest" {
		f.imgRefDigest["my/app:latest"] = "sha256:app999"
	}
	return nil
}

func (f *rolloutFake) ImageInspect(ctx context.Context, ref string) (types.ImageInspect, error) {
	id, ok := f.imgRefDigest[ref]
	if !ok {
		return types.ImageInspect{}, errors.New("unknown image ref")
	}
	return types.ImageInspect{ID: id}, nil
}

func (f *rolloutFake) Stop(ctx context.Context, id string, timeoutSec *int) error {
	f.stopped = append(f.stopped, id)
	return nil
}

func (f *rolloutFake) Remove(ctx context.Context, id string, removeVolumes bool) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *rolloutFake) Create(ctx context.Context, name string, cfg *container.Config, hc *container.HostConfig, nc *network.NetworkingConfig) (string, error) {
	f.created = append(f.created, name)
	return "new-" + name, nil
}

func (f *rolloutFake) Start(ctx context.Context, id string) error {
	f.started = append(f.started, id)
	return nil
}

func (f *rolloutFake) PruneDanglingImages(ctx context.Context) error {
	return nil
}

func TestTick_linkGroup_rolloutOrder(t *testing.T) {
	f := newRolloutFake()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		LabelEnable: false,
		StopTimeout: time.Second,
		Cleanup:     false,
		DryRun:      false,
	}
	m := metrics.New()
	if err := Tick(context.Background(), log, cfg, f, m); err != nil {
		t.Fatal(err)
	}

	wantStop := []string{f.appID, f.dbID}
	if len(f.stopped) != len(wantStop) {
		t.Fatalf("stopped %v want %v", f.stopped, wantStop)
	}
	for i := range wantStop {
		if f.stopped[i] != wantStop[i] {
			t.Fatalf("stop[%d] got %s want %s", i, f.stopped[i], wantStop[i])
		}
	}
	if len(f.removed) != 2 {
		t.Fatalf("removed %v", f.removed)
	}

	if len(f.created) != 2 || f.created[0] != "mydb" || f.created[1] != "myapp" {
		t.Fatalf("create order %v want [mydb myapp]", f.created)
	}
	if len(f.started) != 2 {
		t.Fatalf("started %v", f.started)
	}
}

func TestTick_linkGroup_noChange_noStops(t *testing.T) {
	f := newRolloutFake()
	f.skipDigestBump = true
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		LabelEnable: false,
		StopTimeout: time.Second,
		Cleanup:     false,
	}
	m := metrics.New()
	if err := Tick(context.Background(), log, cfg, f, m); err != nil {
		t.Fatal(err)
	}
	if len(f.stopped) != 0 || len(f.removed) != 0 || len(f.created) != 0 {
		t.Fatalf("expected no rollout, got stopped=%v removed=%v created=%v", f.stopped, f.removed, f.created)
	}
}
