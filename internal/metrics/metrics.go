package metrics

import (
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	namespace = "kran"
)

// Metrics holds Prometheus collectors for kran. Methods are safe to call with a nil receiver.
type Metrics struct {
	reg *prometheus.Registry

	buildInfo *prometheus.GaugeVec

	tickTotal        prometheus.Counter
	tickErrors       prometheus.Counter
	tickDuration     prometheus.Histogram
	lastTickUnix     prometheus.Gauge
	containersScanned prometheus.Gauge
	containersManaged prometheus.Gauge

	pullTotal    *prometheus.CounterVec
	pullDuration prometheus.Histogram

	updateTotal    *prometheus.CounterVec
	updateDuration prometheus.Histogram

	notifyTotal *prometheus.CounterVec
}

// New registers all collectors on a dedicated registry.
func New() *Metrics {
	reg := prometheus.NewRegistry()

	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      "build_info",
			Help:      "Build metadata (labels are constant for this binary).",
		},
		[]string{"version", "commit"},
	)

	tickTotal := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tick_total",
		Help:      "Total number of poll ticks completed.",
	})
	tickErrors := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "tick_errors_total",
		Help:      "Total number of ticks that ended with an error (e.g. list containers failed).",
	})
	tickDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "tick_duration_seconds",
		Help:      "Wall time spent in one tick (list + per-container work).",
		Buckets:   []float64{0.1, 0.5, 1, 5, 15, 60, 300},
	})
	lastTickUnix := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "last_tick_timestamp_seconds",
		Help:      "Unix time of the last successful tick (list succeeded).",
	})
	containersScanned := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "containers_scanned",
		Help:      "Number of running containers seen in the last successful tick.",
	})
	containersManaged := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "containers_managed",
		Help:      "Number of containers eligible for updates in the last successful tick (after Managed filter).",
	})

	pullTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "image_pulls_total",
		Help:      "Total image pulls by result.",
	}, []string{"result"})
	pullDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "image_pull_duration_seconds",
		Help:      "Duration of docker image pull.",
		Buckets:   prometheus.DefBuckets,
	})

	updateTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "updates_total",
		Help:      "Container updates by result (dry_run counts would-have updates).",
	}, []string{"result"})
	updateDuration := prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "update_duration_seconds",
		Help:      "Duration of recreate (stop through start).",
		Buckets:   []float64{0.5, 1, 2, 5, 15, 30, 60, 120, 300},
	})

	notifyTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "notify_notifications_total",
		Help:      "Shoutrrr notification attempts by result.",
	}, []string{"result"})

	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{Namespace: namespace}),
		buildInfo,
		tickTotal,
		tickErrors,
		tickDuration,
		lastTickUnix,
		containersScanned,
		containersManaged,
		pullTotal,
		pullDuration,
		updateTotal,
		updateDuration,
		notifyTotal,
	)

	m := &Metrics{
		reg:               reg,
		buildInfo:       buildInfo,
		tickTotal:        tickTotal,
		tickErrors:       tickErrors,
		tickDuration:     tickDuration,
		lastTickUnix:     lastTickUnix,
		containersScanned: containersScanned,
		containersManaged: containersManaged,
		pullTotal:        pullTotal,
		pullDuration:     pullDuration,
		updateTotal:      updateTotal,
		updateDuration:   updateDuration,
		notifyTotal:      notifyTotal,
	}
	v, c := readBuildInfo()
	m.SetBuildInfo(v, c)
	return m
}

func readBuildInfo() (version, commit string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown", "unknown"
	}
	version = bi.Main.Version
	if version == "" || version == "(devel)" {
		version = "unknown"
	}
	commit = "unknown"
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			commit = s.Value
			if len(commit) > 12 {
				commit = commit[:12]
			}
			break
		}
	}
	return version, commit
}

// Handler exposes Prometheus metrics for mounting on a mux.
func (m *Metrics) Handler() http.Handler {
	if m == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "metrics disabled", http.StatusServiceUnavailable)
		})
	}
	return promhttp.HandlerFor(m.reg, promhttp.HandlerOpts{})
}

// SetBuildInfo sets the kran_build_info gauge (typically from ldflags; overrides auto-detected values).
func (m *Metrics) SetBuildInfo(version, commit string) {
	if m == nil {
		return
	}
	version = strings.TrimSpace(version)
	commit = strings.TrimSpace(commit)
	if version == "" {
		version = "unknown"
	}
	if commit == "" {
		commit = "unknown"
	}
	m.buildInfo.Reset()
	m.buildInfo.WithLabelValues(version, commit).Set(1)
}

// ObserveTick records one poll tick.
func (m *Metrics) ObserveTick(dur time.Duration, scanned, managed int, err error) {
	if m == nil {
		return
	}
	m.tickTotal.Inc()
	m.tickDuration.Observe(dur.Seconds())
	if err != nil {
		m.tickErrors.Inc()
		return
	}
	m.lastTickUnix.Set(float64(time.Now().Unix()))
	m.containersScanned.Set(float64(scanned))
	m.containersManaged.Set(float64(managed))
}

// ObservePull records an image pull attempt (result: success or failure).
func (m *Metrics) ObservePull(result string, dur time.Duration) {
	if m == nil {
		return
	}
	m.pullTotal.WithLabelValues(result).Inc()
	m.pullDuration.Observe(dur.Seconds())
}

// ObserveUpdate records a container update (result: success, failure, or dry_run).
func (m *Metrics) ObserveUpdate(result string, dur time.Duration) {
	if m == nil {
		return
	}
	m.updateTotal.WithLabelValues(result).Inc()
	m.updateDuration.Observe(dur.Seconds())
}

// ObserveNotify records a Shoutrrr notification attempt (result: success or failure).
func (m *Metrics) ObserveNotify(result string) {
	if m == nil {
		return
	}
	m.notifyTotal.WithLabelValues(result).Inc()
}

// Registry returns the underlying prometheus registry (for tests).
func (m *Metrics) Registry() *prometheus.Registry {
	if m == nil {
		return nil
	}
	return m.reg
}
