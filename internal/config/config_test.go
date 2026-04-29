package config

import (
	"os"
	"testing"
	"time"
)

func TestFromArgs_defaults(t *testing.T) {
	t.Setenv("KRAN_INTERVAL", "")
	t.Setenv("DOCKER_HOST", "")
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DockerHost != DefaultDockerHost {
		t.Fatalf("DockerHost: got %q want %q", cfg.DockerHost, DefaultDockerHost)
	}
	if cfg.Interval != 5*time.Minute {
		t.Fatalf("Interval: got %v", cfg.Interval)
	}
	if cfg.StopTimeout != 10*time.Second {
		t.Fatalf("StopTimeout: got %v", cfg.StopTimeout)
	}
}

func TestFromArgs_envInterval(t *testing.T) {
	t.Setenv("KRAN_INTERVAL", "10m")
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 10*time.Minute {
		t.Fatalf("got %v", cfg.Interval)
	}
}

func TestFromArgs_invalidInterval(t *testing.T) {
	t.Setenv("KRAN_INTERVAL", "not-a-duration")
	_, err := FromArgs(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestTruth_envBool(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"", false},
		{"1", true},
		{"true", true},
		{"FALSE", false},
		{"0", false},
	}
	for _, tc := range tests {
		if got := truthy(tc.val); got != tc.want {
			t.Errorf("truthy(%q) = %v, want %v", tc.val, got, tc.want)
		}
	}
}

func TestFromArgs_LabelEnableEnv(t *testing.T) {
	t.Setenv(EnvLabelEnable, "1")
	t.Cleanup(func() { _ = os.Unsetenv(EnvLabelEnable) })
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LabelEnable {
		t.Fatal("expected LabelEnable true")
	}
}
