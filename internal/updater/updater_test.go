package updater

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/glaslos/kran/internal/config"
)

func TestNormalizeImageID(t *testing.T) {
	if g, w := normalizeImageID("sha256:AbCd"), "abcd"; g != w {
		t.Fatalf("got %q want %q", g, w)
	}
	if g, w := normalizeImageID("  sha256:AbCd  "), "abcd"; g != w {
		t.Fatalf("got %q want %q", g, w)
	}
}

func TestManaged(t *testing.T) {
	base := types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name:       "/app",
			HostConfig: &container.HostConfig{},
		},
		Config: &container.Config{
			Image: "nginx:latest",
			Labels: map[string]string{
				config.LabelEnableKey: "true",
			},
		},
	}

	t.Run("ignore label", func(t *testing.T) {
		in := base
		in.Config = copyCfg(base.Config)
		in.Config.Labels[config.LabelIgnoreKey] = "true"
		if Managed(in, &config.Config{}) {
			t.Fatal("expected false")
		}
	})

	t.Run("label enable required", func(t *testing.T) {
		in := base
		in.Config = copyCfg(base.Config)
		delete(in.Config.Labels, config.LabelEnableKey)
		if Managed(in, &config.Config{LabelEnable: true}) {
			t.Fatal("expected false")
		}
		if !Managed(base, &config.Config{LabelEnable: true}) {
			t.Fatal("expected true")
		}
	})

	t.Run("self name", func(t *testing.T) {
		if Managed(base, &config.Config{SelfName: "app"}) {
			t.Fatal("expected false")
		}
		if !Managed(base, &config.Config{SelfName: "other"}) {
			t.Fatal("expected true")
		}
	})
}

func copyCfg(c *container.Config) *container.Config {
	out := *c
	out.Labels = map[string]string{}
	for k, v := range c.Labels {
		out.Labels[k] = v
	}
	return &out
}
