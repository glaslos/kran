package recreate

import (
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

func TestFromInspect_basic(t *testing.T) {
	in := types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name: "/web",
			HostConfig: &container.HostConfig{
				NetworkMode: "bridge",
			},
		},
		Config: &container.Config{
			Image: "nginx:old",
			Env:   []string{"FOO=bar"},
		},
		NetworkSettings: &types.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"bridge": {
					Aliases: []string{"web"},
				},
			},
		},
	}

	p, err := FromInspect(in, "nginx:new")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "web" {
		t.Fatalf("name %q", p.Name)
	}
	if p.Config.Image != "nginx:new" {
		t.Fatalf("image %q", p.Config.Image)
	}
	if p.NetworkingConfig == nil || p.NetworkingConfig.EndpointsConfig["bridge"] == nil {
		t.Fatal("expected bridge endpoint")
	}
}

func TestFromInspect_containerNetworkMode_rejected(t *testing.T) {
	in := types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			Name: "/x",
			HostConfig: &container.HostConfig{
				NetworkMode: "container:abc123",
			},
		},
		Config: &container.Config{Image: "alpine"},
	}
	_, err := FromInspect(in, "alpine:latest")
	if err == nil {
		t.Fatal("expected error")
	}
}
