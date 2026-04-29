package recreate

import (
	"errors"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
)

// Params holds inputs for replacing a container with a new image.
type Params struct {
	Config           *container.Config
	HostConfig       *container.HostConfig
	NetworkingConfig *network.NetworkingConfig
	Name             string
}

// FromInspect builds create parameters from an inspected container, pointing at imageRef.
func FromInspect(in types.ContainerJSON, imageRef string) (*Params, error) {
	if in.ContainerJSONBase == nil || in.Config == nil || in.HostConfig == nil {
		return nil, errors.New("incomplete container inspect data")
	}

	if strings.HasPrefix(string(in.HostConfig.NetworkMode), "container:") {
		return nil, fmt.Errorf("unsupported NetworkMode %q (shared network stack)", in.HostConfig.NetworkMode)
	}

	name := strings.TrimPrefix(in.Name, "/")
	cfg := *in.Config
	cfg.Image = imageRef

	hc := *in.HostConfig

	p := &Params{
		Config:     &cfg,
		HostConfig: &hc,
		Name:       name,
	}
	if in.NetworkSettings != nil && len(in.NetworkSettings.Networks) > 0 {
		p.NetworkingConfig = networkingFromInspect(in.NetworkSettings.Networks)
	}
	return p, nil
}

func networkingFromInspect(endpoints map[string]*network.EndpointSettings) *network.NetworkingConfig {
	out := make(map[string]*network.EndpointSettings, len(endpoints))
	for n, ep := range endpoints {
		out[n] = endpointForCreate(ep)
	}
	return &network.NetworkingConfig{EndpointsConfig: out}
}

func endpointForCreate(ep *network.EndpointSettings) *network.EndpointSettings {
	if ep == nil {
		return nil
	}
	c := ep.Copy()
	// Operational data is reassigned on attach; clear to satisfy API validation.
	c.NetworkID = ""
	c.EndpointID = ""
	c.Gateway = ""
	c.IPAddress = ""
	c.IPPrefixLen = 0
	c.IPv6Gateway = ""
	c.GlobalIPv6Address = ""
	c.GlobalIPv6PrefixLen = 0
	c.MacAddress = ""
	c.DNSNames = nil
	return c
}
