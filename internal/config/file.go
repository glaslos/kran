package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const EnvConfig = "KRAN_CONFIG"

// fileConfig holds optional keys from a YAML/JSON config file.
type fileConfig struct {
	Interval      *string `yaml:"interval"`
	DockerHost    *string `yaml:"docker_host"`
	LabelEnable   *bool   `yaml:"label_enable"`
	SelfName      *string `yaml:"self_name"`
	DryRun        *bool   `yaml:"dry_run"`
	Cleanup       *bool   `yaml:"cleanup"`
	StopTimeout   *string `yaml:"stop_timeout"`
	LogJSON       *bool   `yaml:"log_json"`
	LogLevel      *string `yaml:"log_level"`
	NotifyURL     *string `yaml:"notify_url"`
	HTTPAddr      *string `yaml:"http_addr"`
	WebhookAPIKey *string `yaml:"webhook_api_key"`
}

func loadConfigFile(path string) (*fileConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	var fc fileConfig
	if err := yaml.Unmarshal(b, &fc); err != nil {
		return nil, fmt.Errorf("parse config file %q: %w", path, err)
	}
	return &fc, nil
}

func configPathFromArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-config" || a == "--config":
			if i+1 < len(args) {
				return strings.TrimSpace(args[i+1])
			}
			return ""
		case strings.HasPrefix(a, "-config="):
			return strings.TrimSpace(strings.TrimPrefix(a, "-config="))
		case strings.HasPrefix(a, "--config="):
			return strings.TrimSpace(strings.TrimPrefix(a, "--config="))
		}
	}
	return strings.TrimSpace(os.Getenv(EnvConfig))
}

func parseDurationField(name, raw string) (time.Duration, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("config file: empty %s duration", name)
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("config file: invalid %s %q: %w", name, raw, err)
	}
	return d, nil
}
