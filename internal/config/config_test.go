package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFromArgs_defaults(t *testing.T) {
	t.Setenv("KRAN_INTERVAL", "")
	t.Setenv("DOCKER_HOST", "")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
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
	if cfg.LogLevel != slog.LevelInfo {
		t.Fatalf("LogLevel: got %v want info", cfg.LogLevel)
	}
	if cfg.HTTPAddr != "" {
		t.Fatalf("HTTPAddr: got %q want empty", cfg.HTTPAddr)
	}
	if cfg.WebhookAPIKey != "" {
		t.Fatalf("WebhookAPIKey: got %q want empty", cfg.WebhookAPIKey)
	}
}

func TestFromArgs_envInterval(t *testing.T) {
	t.Setenv("KRAN_INTERVAL", "10m")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
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
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
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

func TestFromArgs_LogLevelEnv(t *testing.T) {
	t.Setenv(EnvLogLevel, "debug")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvLogLevel) })
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Fatalf("LogLevel: got %v want debug", cfg.LogLevel)
	}
}

func TestFromArgs_invalidLogLevel(t *testing.T) {
	t.Setenv(EnvLogLevel, "verbose")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvLogLevel) })
	_, err := FromArgs(nil)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestFromArgs_LabelEnableEnv(t *testing.T) {
	t.Setenv(EnvLabelEnable, "1")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvLabelEnable) })
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LabelEnable {
		t.Fatal("expected LabelEnable true")
	}
}

func TestFromArgs_configFile(t *testing.T) {
	t.Setenv(EnvInterval, "")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvDockerHost, "")
	t.Setenv(EnvLabelEnable, "")
	t.Setenv(EnvSelfName, "")
	t.Setenv(EnvDryRun, "")
	t.Setenv(EnvCleanup, "")
	t.Setenv(EnvStopTimeout, "")
	t.Setenv(EnvLogJSON, "")
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")

	path := filepath.Join(t.TempDir(), "kran.yaml")
	content := `
interval: 30m
docker_host: unix:///custom.sock
label_enable: true
self_name: kran-test
dry_run: true
cleanup: true
stop_timeout: 15s
log_json: true
log_level: warn
http_addr: ":8080"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := FromArgs([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 30*time.Minute {
		t.Fatalf("Interval: got %v", cfg.Interval)
	}
	if cfg.DockerHost != "unix:///custom.sock" {
		t.Fatalf("DockerHost: got %q", cfg.DockerHost)
	}
	if !cfg.LabelEnable || cfg.SelfName != "kran-test" || !cfg.DryRun || !cfg.Cleanup {
		t.Fatalf("unexpected booleans or self-name: %+v", cfg)
	}
	if cfg.StopTimeout != 15*time.Second {
		t.Fatalf("StopTimeout: got %v", cfg.StopTimeout)
	}
	if !cfg.LogJSON || cfg.LogLevel != slog.LevelWarn {
		t.Fatalf("logging: json=%v level=%v", cfg.LogJSON, cfg.LogLevel)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr: got %q", cfg.HTTPAddr)
	}
}

func TestFromArgs_configFile_envOverridesFile(t *testing.T) {
	t.Setenv(EnvInterval, "10m")
	t.Setenv(EnvLogLevel, "")
	t.Setenv(EnvDockerHost, "")
	t.Setenv(EnvLabelEnable, "")
	t.Setenv(EnvSelfName, "")
	t.Setenv(EnvDryRun, "")
	t.Setenv(EnvCleanup, "")
	t.Setenv(EnvStopTimeout, "")
	t.Setenv(EnvLogJSON, "")
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvInterval) })

	path := filepath.Join(t.TempDir(), "kran.yaml")
	if err := os.WriteFile(path, []byte("interval: 45m\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := FromArgs([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Interval != 10*time.Minute {
		t.Fatalf("expected KRAN_INTERVAL to override file, got %v", cfg.Interval)
	}
}

func TestFromArgs_configFile_missing(t *testing.T) {
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvHTTPAddr, "")
	_, err := FromArgs([]string{"-config", filepath.Join(t.TempDir(), "nope.yaml")})
	if err == nil {
		t.Fatal("expected error for missing config file")
	}
}

func TestFromArgs_notifyFromFile(t *testing.T) {
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Setenv(EnvConfig, "")

	path := filepath.Join(t.TempDir(), "kran.yaml")
	content := `
notify_url: gotify://push.example.com/message?token=secret&priority=8
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromArgs([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	want := "gotify://push.example.com/message?token=secret&priority=8"
	if cfg.NotifyURL != want {
		t.Fatalf("NotifyURL: got %q want %q", cfg.NotifyURL, want)
	}
}

func TestFromArgs_httpAddrEnv(t *testing.T) {
	t.Setenv(EnvHTTPAddr, ":9090")
	t.Setenv(EnvNotifyURL, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvHTTPAddr) })
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Fatalf("HTTPAddr: got %q", cfg.HTTPAddr)
	}
}

func TestFromArgs_httpAddrEnvOverridesFile(t *testing.T) {
	t.Setenv(EnvHTTPAddr, ":7777")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvConfig, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvHTTPAddr) })

	path := filepath.Join(t.TempDir(), "kran.yaml")
	if err := os.WriteFile(path, []byte("http_addr: :8080\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromArgs([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":7777" {
		t.Fatalf("expected KRAN_HTTP_ADDR to override file, got %q", cfg.HTTPAddr)
	}
}

func TestFromArgs_webhookAPIKeyEnv(t *testing.T) {
	t.Setenv(EnvWebhookAPIKey, "hunter2")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Cleanup(func() { _ = os.Unsetenv(EnvWebhookAPIKey) })
	cfg, err := FromArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookAPIKey != "hunter2" {
		t.Fatalf("WebhookAPIKey: got %q", cfg.WebhookAPIKey)
	}
}

func TestFromArgs_webhookAPIKeyFromFile(t *testing.T) {
	t.Setenv(EnvConfig, "")
	t.Setenv(EnvNotifyURL, "")
	t.Setenv(EnvHTTPAddr, "")
	t.Setenv(EnvWebhookAPIKey, "")

	path := filepath.Join(t.TempDir(), "kran.yaml")
	if err := os.WriteFile(path, []byte("webhook_api_key: from-yaml\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := FromArgs([]string{"-config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebhookAPIKey != "from-yaml" {
		t.Fatalf("WebhookAPIKey: got %q", cfg.WebhookAPIKey)
	}
}
