package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	EnvInterval    = "KRAN_INTERVAL"
	EnvDockerHost  = "DOCKER_HOST"
	EnvLabelEnable = "KRAN_LABEL_ENABLE"
	EnvSelfName    = "KRAN_SELF_NAME"
	EnvDryRun      = "KRAN_DRY_RUN"
	EnvCleanup     = "KRAN_CLEANUP"
	EnvStopTimeout = "KRAN_STOP_TIMEOUT"
	EnvLogJSON     = "KRAN_LOG_JSON"
	EnvLogLevel    = "KRAN_LOG_LEVEL"
	EnvNotifyURL   = "KRAN_NOTIFY_URL"
	EnvHTTPAddr    = "KRAN_HTTP_ADDR"
)

const DefaultDockerHost = "unix:///var/run/docker.sock"

const LabelEnableKey = "kran.enable"
const LabelIgnoreKey = "kran.ignore"

// Config holds runtime options for kran.
type Config struct {
	Interval    time.Duration
	DockerHost  string
	LabelEnable bool
	SelfName    string
	DryRun      bool
	Cleanup     bool
	StopTimeout time.Duration
	LogJSON     bool
	LogLevel    slog.Level
	// NotifyURL is a comma-separated list of Shoutrrr service URLs (e.g. gotify://host/message?token=…).
	NotifyURL string
	// HTTPAddr is the listen address for the HTTP API (e.g. ":9090"). Empty disables the server.
	HTTPAddr string
}

// FromArgs parses os.Args[1:] into Config.
func FromArgs(args []string) (*Config, error) {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			printUsage()
			return nil, flag.ErrHelp
		}
	}

	var fc *fileConfig
	configPath := configPathFromArgs(args)
	if configPath != "" {
		var err error
		fc, err = loadConfigFile(configPath)
		if err != nil {
			return nil, err
		}
	}

	intervalDef := 5 * time.Minute
	if fc != nil && fc.Interval != nil && strings.TrimSpace(*fc.Interval) != "" {
		d, err := parseDurationField("interval", *fc.Interval)
		if err != nil {
			return nil, err
		}
		intervalDef = d
	}

	dockerHostDef := DefaultDockerHost
	if fc != nil && fc.DockerHost != nil && strings.TrimSpace(*fc.DockerHost) != "" {
		dockerHostDef = strings.TrimSpace(*fc.DockerHost)
	}
	dockerHostDef = envOr(EnvDockerHost, dockerHostDef)

	labelEnableDef := false
	if v := os.Getenv(EnvLabelEnable); v != "" {
		labelEnableDef = truthy(v)
	} else if fc != nil && fc.LabelEnable != nil {
		labelEnableDef = *fc.LabelEnable
	}

	selfNameDef := ""
	if v := os.Getenv(EnvSelfName); v != "" {
		selfNameDef = strings.TrimSpace(v)
	} else if fc != nil && fc.SelfName != nil {
		selfNameDef = strings.TrimSpace(*fc.SelfName)
	}

	dryRunDef := false
	if v := os.Getenv(EnvDryRun); v != "" {
		dryRunDef = truthy(v)
	} else if fc != nil && fc.DryRun != nil {
		dryRunDef = *fc.DryRun
	}

	cleanupDef := false
	if v := os.Getenv(EnvCleanup); v != "" {
		cleanupDef = truthy(v)
	} else if fc != nil && fc.Cleanup != nil {
		cleanupDef = *fc.Cleanup
	}

	stopTimeoutDef := 10 * time.Second
	if fc != nil && fc.StopTimeout != nil && strings.TrimSpace(*fc.StopTimeout) != "" {
		d, err := parseDurationField("stop_timeout", *fc.StopTimeout)
		if err != nil {
			return nil, err
		}
		stopTimeoutDef = d
	}

	logJSONDef := false
	if v := os.Getenv(EnvLogJSON); v != "" {
		logJSONDef = truthy(v)
	} else if fc != nil && fc.LogJSON != nil {
		logJSONDef = *fc.LogJSON
	}

	logLevelDef := "info"
	if fc != nil && fc.LogLevel != nil && strings.TrimSpace(*fc.LogLevel) != "" {
		logLevelDef = strings.TrimSpace(*fc.LogLevel)
	}

	notifyURLDef := ""
	if fc != nil && fc.NotifyURL != nil {
		notifyURLDef = strings.TrimSpace(*fc.NotifyURL)
	}
	notifyURLDef = envOr(EnvNotifyURL, notifyURLDef)

	httpAddrDef := ""
	if fc != nil && fc.HTTPAddr != nil {
		httpAddrDef = strings.TrimSpace(*fc.HTTPAddr)
	}
	httpAddrDef = envOr(EnvHTTPAddr, httpAddrDef)

	fs := flag.NewFlagSet("kran", flag.ContinueOnError)
	fs.String("config", "", "path to YAML or JSON config file (or "+EnvConfig+")")
	var (
		interval    = fs.Duration("interval", intervalDef, "poll interval (e.g. 5m, 24h)")
		dockerHost  = fs.String("docker-host", dockerHostDef, "Docker daemon address (or "+EnvDockerHost+")")
		labelEnable = fs.Bool("label-enable", labelEnableDef, "only update containers with label "+LabelEnableKey+"=true (or "+EnvLabelEnable+"=1)")
		selfName    = fs.String("self-name", selfNameDef, "container name to exclude (this updater), without leading slash (or "+EnvSelfName+")")
		dryRun      = fs.Bool("dry-run", dryRunDef, "log actions only, do not change containers")
		cleanup     = fs.Bool("cleanup", cleanupDef, "after a successful recreate: remove anonymous volumes from the old container and prune dangling images")
		stopTimeout = fs.Duration("stop-timeout", stopTimeoutDef, "SIGTERM grace period before SIGKILL (or "+EnvStopTimeout+")")
		logJSON     = fs.Bool("log-json", logJSONDef, "emit logs as JSON (or "+EnvLogJSON+"=1)")
		logLevel    = fs.String("log-level", logLevelDef, "log verbosity: debug, info, warn, error (or "+EnvLogLevel+")")
		notifyURL   = fs.String("notify-url", notifyURLDef, "comma-separated Shoutrrr URLs (or "+EnvNotifyURL+"); see https://containrrr.dev/shoutrrr/")
		httpAddr    = fs.String("http-addr", httpAddrDef, "HTTP listen address for /healthz and /metrics (or "+EnvHTTPAddr+"); empty to disable")
	)

	fs.Usage = printUsage

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if v := os.Getenv(EnvInterval); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("invalid " + EnvInterval + ": " + err.Error())
		}
		*interval = d
	}
	if v := os.Getenv(EnvLabelEnable); v != "" {
		*labelEnable = truthy(v)
	}
	if v := os.Getenv(EnvStopTimeout); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, errors.New("invalid " + EnvStopTimeout + ": " + err.Error())
		}
		*stopTimeout = d
	}
	if v := os.Getenv(EnvNotifyURL); v != "" {
		*notifyURL = strings.TrimSpace(v)
	}
	if v := os.Getenv(EnvHTTPAddr); v != "" {
		*httpAddr = strings.TrimSpace(v)
	}

	logLevelStr := strings.TrimSpace(*logLevel)
	if v := strings.TrimSpace(os.Getenv(EnvLogLevel)); v != "" {
		logLevelStr = v
	}
	parsedLevel, err := parseLogLevel(logLevelStr)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		Interval:    *interval,
		DockerHost:  *dockerHost,
		LabelEnable: *labelEnable,
		SelfName:    strings.TrimSpace(*selfName),
		DryRun:      *dryRun,
		Cleanup:     *cleanup,
		StopTimeout: *stopTimeout,
		LogJSON:     *logJSON,
		LogLevel:    parsedLevel,
		NotifyURL:   strings.TrimSpace(*notifyURL),
		HTTPAddr:    strings.TrimSpace(*httpAddr),
	}

	if cfg.Interval < time.Second {
		return nil, errors.New("interval must be at least 1s")
	}
	return cfg, nil
}

func printUsage() {
	out := os.Stderr
	fmt.Fprintln(out, "kran — periodically pull container images and recreate containers when the digest changes.")
	fmt.Fprintln(out, "Requires Docker socket access (e.g. -v /var/run/docker.sock:/var/run/docker.sock).")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage: kran [flags]")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Flags:")
	fmt.Fprintln(out, "  -config path")
	fmt.Fprintln(out, "    YAML or JSON file with settings (env "+EnvConfig+"); see README")
	fmt.Fprintln(out, "  -interval duration")
	fmt.Fprintln(out, "    poll interval (default 5m0s)")
	fmt.Fprintln(out, "  -docker-host string")
	fmt.Fprintln(out, "    Docker daemon address (env DOCKER_HOST, default unix:///var/run/docker.sock)")
	fmt.Fprintln(out, "  -label-enable")
	fmt.Fprintln(out, "    only update containers with label "+LabelEnableKey+"=true")
	fmt.Fprintln(out, "  -self-name string")
	fmt.Fprintln(out, "    exclude this container name (the updater), without leading slash")
	fmt.Fprintln(out, "  -dry-run")
	fmt.Fprintln(out, "    pull and compare but do not recreate")
	fmt.Fprintln(out, "  -cleanup")
	fmt.Fprintln(out, "    prune dangling images after a successful recreate")
	fmt.Fprintln(out, "  -stop-timeout duration")
	fmt.Fprintln(out, "    SIGTERM grace before SIGKILL (default 10s)")
	fmt.Fprintln(out, "  -log-json")
	fmt.Fprintln(out, "    JSON logs (env KRAN_LOG_JSON)")
	fmt.Fprintln(out, "  -log-level string")
	fmt.Fprintln(out, "    debug, info, warn, error (env "+EnvLogLevel+", default info)")
	fmt.Fprintln(out, "  -notify-url string")
	fmt.Fprintln(out, "    comma-separated Shoutrrr URLs (env "+EnvNotifyURL+"); e.g. gotify://host/message?token=…")
	fmt.Fprintln(out, "  -http-addr string")
	fmt.Fprintln(out, "    HTTP listen address for /healthz and /metrics (env "+EnvHTTPAddr+"); empty to disable")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Environment: "+EnvConfig+", KRAN_INTERVAL, DOCKER_HOST, KRAN_LABEL_ENABLE, KRAN_SELF_NAME,")
	fmt.Fprintln(out, "  KRAN_DRY_RUN, KRAN_CLEANUP, KRAN_STOP_TIMEOUT, KRAN_LOG_JSON, "+EnvLogLevel+", "+EnvNotifyURL+", "+EnvHTTPAddr)
}

func parseLogLevel(s string) (slog.Level, error) {
	if s == "" {
		return 0, errors.New("empty log level")
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(strings.ToUpper(strings.TrimSpace(s)))); err != nil {
		return 0, fmt.Errorf("invalid log level %q (use debug, info, warn, error): %w", s, err)
	}
	return lvl, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func truthy(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return false
	}
	switch s {
	case "1", "true", "yes", "on":
		return true
	default:
		if n, err := strconv.Atoi(s); err == nil && n != 0 {
			return true
		}
		return false
	}
}
