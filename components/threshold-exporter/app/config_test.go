package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestResolve_ThreeState(t *testing.T) {
	cfg := &ThresholdConfig{
		Defaults: map[string]float64{
			"mysql_connections": 80,
			"mysql_cpu":         80,
		},
		Tenants: map[string]map[string]string{
			"db-a": {
				"mysql_connections": "70",
				// mysql_cpu omitted → default 80
			},
			"db-b": {
				"mysql_connections": "disable",
				"mysql_cpu":         "40",
			},
		},
	}

	resolved := cfg.Resolve()
	// Sort for deterministic comparison
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].Tenant != resolved[j].Tenant {
			return resolved[i].Tenant < resolved[j].Tenant
		}
		return resolved[i].Metric < resolved[j].Metric
	})

	// Expected:
	// db-a: connections=70 (custom), cpu=80 (default)
	// db-b: connections=SKIP (disabled), cpu=40 (custom)
	expected := []struct {
		tenant, metric, component string
		value                     float64
	}{
		{"db-a", "connections", "mysql", 70},
		{"db-a", "cpu", "mysql", 80},
		{"db-b", "cpu", "mysql", 40},
	}

	if len(resolved) != len(expected) {
		t.Fatalf("expected %d resolved thresholds, got %d: %+v", len(expected), len(resolved), resolved)
	}

	for i, exp := range expected {
		r := resolved[i]
		if r.Tenant != exp.tenant || r.Metric != exp.metric || r.Component != exp.component || r.Value != exp.value {
			t.Errorf("index %d: expected {%s %s %s %.0f}, got {%s %s %s %.0f}",
				i, exp.tenant, exp.metric, exp.component, exp.value,
				r.Tenant, r.Metric, r.Component, r.Value)
		}
	}
}

func TestResolve_DisableVariants(t *testing.T) {
	cfg := &ThresholdConfig{
		Defaults: map[string]float64{"mysql_connections": 80},
		Tenants: map[string]map[string]string{
			"t1": {"mysql_connections": "disable"},
			"t2": {"mysql_connections": "disabled"},
			"t3": {"mysql_connections": "off"},
			"t4": {"mysql_connections": "false"},
			"t5": {"mysql_connections": "DISABLE"},
		},
	}

	resolved := cfg.Resolve()
	if len(resolved) != 0 {
		t.Errorf("expected 0 resolved thresholds for disabled variants, got %d: %+v", len(resolved), resolved)
	}
}

func TestResolve_CustomSeverity(t *testing.T) {
	cfg := &ThresholdConfig{
		Defaults: map[string]float64{"mysql_connections": 80},
		Tenants: map[string]map[string]string{
			"db-a": {"mysql_connections": "50:critical"},
		},
	}

	resolved := cfg.Resolve()
	if len(resolved) != 1 {
		t.Fatalf("expected 1, got %d", len(resolved))
	}
	if resolved[0].Value != 50 || resolved[0].Severity != "critical" {
		t.Errorf("expected value=50 severity=critical, got value=%.0f severity=%s", resolved[0].Value, resolved[0].Severity)
	}
}

func TestResolve_EmptyTenants(t *testing.T) {
	cfg := &ThresholdConfig{
		Defaults: map[string]float64{"mysql_connections": 80},
		Tenants:  map[string]map[string]string{},
	}

	resolved := cfg.Resolve()
	if len(resolved) != 0 {
		t.Errorf("expected 0 (no tenants), got %d", len(resolved))
	}
}

func TestResolve_TenantWithNoOverrides(t *testing.T) {
	cfg := &ThresholdConfig{
		Defaults: map[string]float64{
			"mysql_connections": 80,
			"mysql_cpu":         90,
		},
		Tenants: map[string]map[string]string{
			"db-a": {}, // no overrides → all defaults
		},
	}

	resolved := cfg.Resolve()
	if len(resolved) != 2 {
		t.Fatalf("expected 2, got %d", len(resolved))
	}

	for _, r := range resolved {
		if r.Tenant != "db-a" {
			t.Errorf("unexpected tenant: %s", r.Tenant)
		}
	}
}

func TestParseMetricKey(t *testing.T) {
	tests := []struct {
		input              string
		wantComp, wantMet string
	}{
		{"mysql_connections", "mysql", "connections"},
		{"mysql_cpu", "mysql", "cpu"},
		{"container_cpu_percent", "container", "cpu_percent"},
		{"standalone", "default", "standalone"},
	}

	for _, tt := range tests {
		comp, met := parseMetricKey(tt.input)
		if comp != tt.wantComp || met != tt.wantMet {
			t.Errorf("parseMetricKey(%q) = (%q, %q), want (%q, %q)",
				tt.input, comp, met, tt.wantComp, tt.wantMet)
		}
	}
}

func TestConfigManager_LoadFile(t *testing.T) {
	content := `
defaults:
  mysql_connections: 80
  mysql_cpu: 80
tenants:
  db-a:
    mysql_connections: "70"
  db-b:
    mysql_connections: "disable"
    mysql_cpu: "40"
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	mgr := NewConfigManager(path)
	if err := mgr.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if !mgr.IsLoaded() {
		t.Error("expected IsLoaded() = true")
	}

	cfg := mgr.GetConfig()
	if len(cfg.Defaults) != 2 {
		t.Errorf("expected 2 defaults, got %d", len(cfg.Defaults))
	}
	if len(cfg.Tenants) != 2 {
		t.Errorf("expected 2 tenants, got %d", len(cfg.Tenants))
	}
}
