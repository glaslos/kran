package updater

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/glaslos/kran/internal/config"
	"github.com/glaslos/kran/internal/linkgroup"
	"github.com/glaslos/kran/internal/registryconfig"
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

func TestPrivateRegistryHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		ref         string
		wantHost    string
		wantPrivate bool
	}{
		{ref: "nginx:latest", wantPrivate: false},
		{ref: "docker.io/library/nginx:latest", wantPrivate: false},
		{ref: "ghcr.io/glaslos/kran:latest", wantHost: "ghcr.io", wantPrivate: true},
		{ref: "localhost:5000/my/app:1", wantHost: "localhost:5000", wantPrivate: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.ref, func(t *testing.T) {
			gotHost, gotPrivate := privateRegistryHost(tt.ref)
			if gotHost != tt.wantHost || gotPrivate != tt.wantPrivate {
				t.Errorf("privateRegistryHost(%q) = (%q,%v), want (%q,%v)", tt.ref, gotHost, gotPrivate, tt.wantHost, tt.wantPrivate)
			}
		})
	}
}

func TestMonitoredPrivateRegistries(t *testing.T) {
	t.Parallel()

	members := []linkgroup.Member{
		linkgroup.NewMember("1", types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{Name: "/a"},
			Config:            &container.Config{Image: "ghcr.io/glaslos/kran:latest"},
		}),
		linkgroup.NewMember("2", types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{Name: "/b"},
			Config:            &container.Config{Image: "nginx:latest"},
		}),
		linkgroup.NewMember("3", types.ContainerJSON{
			ContainerJSONBase: &types.ContainerJSONBase{Name: "/c"},
			Config:            &container.Config{Image: "localhost:5000/app:latest"},
		}),
	}

	got := monitoredPrivateRegistries(members)
	if len(got) != 2 || got[0] != "ghcr.io" || got[1] != "localhost:5000" {
		t.Fatalf("monitoredPrivateRegistries() = %v, want [ghcr.io localhost:5000]", got)
	}
}

func TestReadDockerAuthInfo(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		t.Setenv("DOCKER_CONFIG", t.TempDir())
		sum, err := registryconfig.Summarize()
		if err != nil {
			t.Fatal(err)
		}
		if sum.HasAnyCredentials {
			t.Fatal("expected no credentials")
		}
		if len(sum.AuthHosts) != 0 {
			t.Fatalf("expected no auth hosts, got %v", sum.AuthHosts)
		}
	})

	t.Run("auth entries and helpers", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("DOCKER_CONFIG", dir)
		content := `{
			"auths": {
				"https://ghcr.io": {"auth": "Zm9vOmJhcg=="},
				"index.docker.io/v1/": {}
			},
			"credHelpers": {"ecr.example.com":"ecr-login"}
		}`
		if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		sum, err := registryconfig.Summarize()
		if err != nil {
			t.Fatal(err)
		}
		if !sum.HasAnyCredentials {
			t.Fatal("expected credentials to be detected")
		}
		if _, ok := sum.AuthHosts["ghcr.io"]; !ok {
			t.Fatalf("expected ghcr.io auth host, got %v", sum.AuthHosts)
		}
		if _, ok := sum.AuthHosts["docker.io"]; !ok {
			t.Fatalf("expected docker.io auth host normalization, got %v", sum.AuthHosts)
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
