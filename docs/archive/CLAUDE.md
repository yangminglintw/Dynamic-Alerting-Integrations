專案現狀 (Week 4 Focus)
驗證 Multi-Tenant Dynamic Alerting 架構。目前已完成 Scenario A/B/C，正準備實作 Scenario D (Composite Priority)。

叢集架構 (Kind)
Namespaces: db-a, db-b (Tenants), monitoring (Infrastructure)

Components:

mariadb: Sidecar 模式 (MariaDB 11 + mysqld-exporter)

prometheus: 核心監控，含 Normalization Layer (Recording Rules)

threshold-exporter: Config-driven (YAML ConfigMap 掛載)，支援三態邏輯

kube-state-metrics: 提供 K8s 狀態指標 (Scenario C)

關鍵端點
Prometheus: localhost:9090

Threshold-Exporter Metrics: localhost:8080/metrics

Threshold-Exporter Config: localhost:8080/api/v1/config

技術規範與模式
1. 三態閾值邏輯 (Threshold Logic)
所有閾值透過 components/threshold-exporter/templates/configmap.yaml 管理：

Custom: "70" (特定數值)

Default: 省略鍵值 (套用 defaults 區塊)

Disable: "disable" (不暴露指標 = 不觸發 Alert)

2. 指標命名慣例
原始指標: user_threshold{tenant, metric, component, severity}

狀態旗標: user_state_filter{tenant, filter, severity} (1=啟動, 缺失=停用)

標準化層: tenant:<component>_<metric>:<func> (e.g., tenant:mysql_cpu_usage:rate5m)

3. 開發工作流 (Makefile)
make setup: 一鍵重建環境

make component-build COMP=...: 構建並加載映像檔至 Kind

make component-deploy COMP=...: 部署組件 (自動 reload config)

make test-scenario-<a>|<b>|<c>: 執行端到端驗證

待辦事項 (Scenario D)
[ ] 實作 Composite Priority Logic：支援 condition-specific rules + fallback。

[ ] 引入 unless 排除已匹配條件，or 做聯集處理。

[ ] 整合測試自動化與引入 Tilt。

禁止事項 (Anti-Patterns)
禁止 使用已廢棄的 components/threshold-exporter/*.yaml (請改用 templates/)。

禁止 在 Prometheus Recording Rules 中寫死 Fallback 邏輯 (由 exporter 處理)。

禁止 修改 _state_ 開頭以外的 key 用於狀態比對。