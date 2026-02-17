# Changelog

## [Unreleased] - Week 4: Composite Priority Logic
- **Goal**: Implement Scenario D (Composite Priority).
- **Plan**: Support condition-specific rules + fallback using `unless` / `or` logic.

## [Week 3] - State Matching & Weakest Link (2025-02-23)
### Features
- **Scenario C (State Matching)**:
  - Implemented `user_state_filter` metric (1.0 = enabled).
  - Alert Logic: `count * flag > 0` (Multiplication pattern).
  - Config: Added `state_filters` section and `_state_` prefix for per-tenant disable.
- **Scenario B (Weakest Link)**:
  - Integrated `kubelet-cadvisor` for container metrics.
  - Implemented `tenant:pod_weakest_cpu_percent:max` recording rules.
  - Added container-level thresholds to `threshold-exporter`.

## [Week 2] - Config-Driven Architecture (2025-02-16)
### Refactor
- **Threshold Exporter**:
  - Moved from HTTP API to **YAML ConfigMap + Hot-reload**.
  - Implemented **Three-State Logic**: Custom Value / Default / Disable.
  - Removed per-tenant sidecars to avoid scalability issues.
- **Helm**: Refactored `threshold-exporter` into a full Helm chart with `checksum/config` auto-restart.

## [Week 1] - Foundation (2025-02-09)
### Setup
- **Renaming**: Project renamed to `dynamic-alerting-integrations`.
- **Normalization**: Established Prometheus Recording Rules layer (e.g., `tenant:mysql_cpu_usage:rate5m`).
- **Skills**: Created `diagnose-tenant` script for automated health checks.
- **Infrastructure**: Setup Kind cluster, MariaDB sidecars, and basic Monitoring stack.