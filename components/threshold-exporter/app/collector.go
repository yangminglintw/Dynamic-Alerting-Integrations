package main

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ThresholdCollector implements prometheus.Collector.
// On each scrape, it resolves the current config and exposes:
//   - user_threshold gauge metrics (Scenario A: numeric thresholds)
//   - user_state_filter gauge metrics (Scenario C: state matching flags)
type ThresholdCollector struct {
	manager *ConfigManager

	// Metric descriptors
	thresholdDesc   *prometheus.Desc
	stateFilterDesc *prometheus.Desc
}

func NewThresholdCollector(manager *ConfigManager) *ThresholdCollector {
	return &ThresholdCollector{
		manager: manager,
		thresholdDesc: prometheus.NewDesc(
			"user_threshold",
			"User-defined alerting threshold (config-driven, three-state: custom/default/disable)",
			[]string{"tenant", "metric", "component", "severity"},
			nil,
		),
		stateFilterDesc: prometheus.NewDesc(
			"user_state_filter",
			"State-based monitoring filter flag (1=enabled, absent=disabled). Scenario C: state/string matching.",
			[]string{"tenant", "filter", "severity"},
			nil,
		),
	}
}

// Describe implements prometheus.Collector.
func (c *ThresholdCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.thresholdDesc
	ch <- c.stateFilterDesc
}

// Collect implements prometheus.Collector.
// Called on every /metrics scrape — resolves config in real-time.
func (c *ThresholdCollector) Collect(ch chan<- prometheus.Metric) {
	cfg := c.manager.GetConfig()
	if cfg == nil {
		return
	}

	// Scenario A: numeric thresholds
	for _, t := range cfg.Resolve() {
		ch <- prometheus.MustNewConstMetric(
			c.thresholdDesc,
			prometheus.GaugeValue,
			t.Value,
			t.Tenant,
			t.Metric,
			t.Component,
			t.Severity,
		)
	}

	// Scenario C: state filter flags
	for _, sf := range cfg.ResolveStateFilters() {
		ch <- prometheus.MustNewConstMetric(
			c.stateFilterDesc,
			prometheus.GaugeValue,
			1.0, // flag: 1 = enabled, absent = disabled
			sf.Tenant,
			sf.FilterName,
			sf.Severity,
		)
	}
}

// MetricsHandler returns an HTTP handler that serves /metrics
// with both default Go metrics and our custom threshold collector.
func (c *ThresholdCollector) MetricsHandler() http.Handler {
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	// Also register default Go collector for process metrics
	reg.MustRegister(prometheus.NewGoCollector())

	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		EnableOpenMetrics: false,
	})
}
