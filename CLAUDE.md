# CLAUDE.md — AI 開發上下文指引

## 專案概述
驗證 **Multi-Tenant Dynamic Alerting** 架構。
**當前進度**: Week 4 - Scenario D (Composite Priority Logic).

## 架構現狀 (Current Architecture)
- **Cluster**: Kind (`dynamic-alerting-cluster`)
- **Namespaces**: 
  - `db-a`, `db-b`: Tenant Environments (MariaDB + mysqld-exporter sidecar)
  - `monitoring`: Infrastructure (Prometheus, Grafana, Alertmanager, Custom Exporters)

### 核心組件 (Components)
1. **threshold-exporter** (Port 8080)
   - **職責**: Scenario A/B 的核心，將 YAML 配置轉換為 Prometheus Metrics。
   - **機制**: Config-driven (ConfigMap掛載), Hot-reload (無需重啟)。
   - **設定檔**: `components/threshold-exporter/config/threshold-config.yaml`
2. **kube-state-metrics**
   - **職責**: Scenario C 的核心，提供 K8s Pod/Container 狀態指標。
3. **Prometheus Normalization Layer** (Recording Rules)
   - **職責**: 統一不同來源的指標命名，簡化 Alert Rules。
   - **格式**: `tenant:<component>_<metric>:<function>`

## 開發規範 (Development Rules)

### 1. 三態閾值邏輯 (Three-State Logic)
在 `threshold-config.yaml` 中，每個指標必須符合：
- **Custom**: 明確數值 (e.g., `"70"`) → 覆蓋預設。
- **Default**: 省略 Key → 使用 `defaults` 區塊數值。
- **Disable**: `"disable"` → 不暴露 Metric → Alert Rule 因 `group_left` 匹配失敗而不觸發。

### 2. Alert Rule 設計模式
- **Dynamic Threshold**: 使用 `group_left(tenant)` 關聯 normalized metrics 與 `user_threshold`。
- **State Matching**: 使用乘法邏輯 `(metric_count * user_state_filter) > 0`。
- **Labeling**: 所有 Metrics 必須包含 `tenant` label 以支援多租戶隔離。

### 3. 操作指令 (Makefile)
- **部署組件**: `make component-deploy COMP=threshold-exporter ENV=local`
- **構建映像**: `make component-build COMP=threshold-exporter`
- **執行測試**: `make test-scenario-a` / `make test-scenario-b` / `make test-scenario-c`
- **健康檢查**: `make inspect-tenant TENANT=db-a`
- **Port-Forward**: `make port-forward` (開啟 9090, 3000, 8080)

## AI Agent 環境 (MCP Connectivity)
- **Kubernetes MCP Server**: 可用。Context: `kind-dynamic-alerting-cluster`。
  - 支援: `kubectl_get`, `kubectl_apply`, `exec_in_pod`, `kubectl_logs`, `kubectl_scale`, `kubectl_patch` 等全功能。
  - **動態測試路徑**: exec 進 Prometheus Pod → `wget -qO-` 查詢 API (無需 port-forward)。
  - **Prometheus 查詢**: `exec_in_pod` → `wget -qO- "http://localhost:9090/api/v1/query?query=<PromQL>"`
  - **Exporter 查詢**: `exec_in_pod` → `wget -qO- "http://threshold-exporter.monitoring.svc:8080/metrics"`
- **Windows-MCP**: 可用，但 kubectl/kind 不在 Windows PATH (僅在 Dev Container 內)。
  - 用途限於: 檔案操作、PowerShell 指令、Docker Desktop 狀態。
- **注意**: kubeconfig 需從 Dev Container 匯出至 Windows `%USERPROFILE%\.kube\config` 才能讓 Kubernetes MCP 連線。

## 禁止事項 (Anti-Patterns)
1. **禁止**修改已廢棄的 `components/threshold-exporter/*.yaml` (請修改 `templates/`)。
2. **禁止**在 Go code 中寫死 Tenant ID，必須保持租戶無關 (Tenant-agnostic)。
3. **禁止**在 Recording Rules 中包含 Fallback 邏輯 (Default resolution 應由 Exporter 處理)。

## 下一步 (Week 4 Focus)
實作 **Composite Priority Logic** (Scenario D)：
- 目標：支援條件優先級 (Condition-specific rules) 與 Fallback。
- 關鍵字：PromQL `unless`, `or`, 優先級標籤。