package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestHandler_exposesKranMetrics(t *testing.T) {
	m := New()
	m.ObserveTick(time.Millisecond, 3, 2, nil)
	m.ObservePull("success", 10*time.Millisecond)
	m.ObserveUpdate("dry_run", 0)
	m.ObserveNotify("success")

	ts := httptest.NewServer(m.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, name := range []string{
		`kran_build_info`,
		`kran_tick_total`,
		`kran_image_pulls_total`,
		`kran_updates_total`,
		`kran_notify_notifications_total`,
	} {
		if !strings.Contains(s, name) {
			t.Errorf("metrics body missing %q", name)
		}
	}
}

func TestObserveTick_counters(t *testing.T) {
	m := New()
	if n := testutil.ToFloat64(m.tickTotal); n != 0 {
		t.Fatalf("tick_total before: %v", n)
	}
	m.ObserveTick(time.Millisecond, 1, 1, nil)
	if n := testutil.ToFloat64(m.tickTotal); n != 1 {
		t.Fatalf("tick_total after success: %v", n)
	}
	m.ObserveTick(time.Millisecond, 0, 0, io.EOF)
	if n := testutil.ToFloat64(m.tickTotal); n != 2 {
		t.Fatalf("tick_total after error tick: %v", n)
	}
	if n := testutil.ToFloat64(m.tickErrors); n != 1 {
		t.Fatalf("tick_errors: %v", n)
	}
}

func TestNilMetrics_noPanic(t *testing.T) {
	var m *Metrics
	m.Handler()
	m.SetBuildInfo("v", "c")
	m.ObserveTick(0, 0, 0, nil)
	m.ObservePull("success", 0)
	m.ObserveUpdate("failure", 0)
	m.ObserveNotify("failure")
}
