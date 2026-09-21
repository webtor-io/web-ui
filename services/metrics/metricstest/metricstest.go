// Package metricstest reads the default Prometheus registry for tests of the
// code that reports into it (the router's recovery, the job runner), which
// cannot be handed a private registry the way the metrics package's own tests
// are.
package metricstest

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Counter returns the value of the counter series name{labels}, or 0 when
// the series does not exist yet.
func Counter(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	m := find(t, name, labels)
	if m == nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

// Gauge returns the value of the gauge name, or 0 when it was never touched.
func Gauge(t *testing.T, name string) float64 {
	t.Helper()
	m := find(t, name, nil)
	if m == nil {
		return 0
	}
	return m.GetGauge().GetValue()
}

func find(t *testing.T, name string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if matches(m, labels) {
				return m
			}
		}
	}
	return nil
}

func matches(m *dto.Metric, labels map[string]string) bool {
	got := map[string]string{}
	for _, p := range m.GetLabel() {
		got[p.GetName()] = p.GetValue()
	}
	if len(got) != len(labels) {
		return false
	}
	for k, v := range labels {
		if got[k] != v {
			return false
		}
	}
	return true
}
