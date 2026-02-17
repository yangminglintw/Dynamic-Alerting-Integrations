# Threshold Exporter

**核心 Component** — 集中式、config-driven 的 Prometheus metric exporter，將使用者設定的動態閾值轉換為 Prometheus metrics，實現 Scenario A (Dynamic Thresholds)。

## 架構

- **單一 Pod** 在 monitoring namespace，服務所有 tenant
- **YAML config** 透過 ConfigMap 掛載，定義 defaults + per-tenant overrides
- **三態設計**: custom value / default / disable
- **Hot-reload**: 定期檢查 config 變更，不需重啟 Pod

## Config 格式

```yaml
defaults:
  mysql_connections: 80     # 所有 tenant 的預設值
  mysql_cpu: 80

tenants:
  db-a:
    mysql_connections: "70"       # custom → 暴露 70
    # mysql_cpu 省略               # → 使用 default 80
  db-b:
    mysql_connections: "disable"  # disabled → 不暴露，不觸發 alert
    mysql_cpu: "40:critical"      # custom + severity override
```

### 三態行為

| 設定 | 行為 | Prometheus 輸出 |
|------|------|-----------------|
| `"70"` | Custom value | `user_threshold{...} 70` |
| 省略不寫 | Use default | `user_threshold{...} 80` |
| `"disable"` | Disabled | 不產生 metric |

## Endpoints

| Path | 說明 |
|------|------|
| `GET /metrics` | Prometheus metrics (user_threshold gauge) |
| `GET /health` | Liveness probe |
| `GET /ready` | Readiness probe (config loaded?) |
| `GET /api/v1/config` | 查看當前 config 與 resolved thresholds (debug) |

## Metrics 輸出格式

```prometheus
# HELP user_threshold User-defined alerting threshold (config-driven)
# TYPE user_threshold gauge
user_threshold{tenant="db-a",component="mysql",metric="connections",severity="warning"} 70
user_threshold{tenant="db-a",component="mysql",metric="cpu",severity="warning"} 80
user_threshold{tenant="db-b",component="mysql",metric="cpu",severity="critical"} 40
```

## Prometheus 整合

Recording rules 直接透傳 exporter 的 resolved values（無 fallback 邏輯）：

```yaml
- record: tenant:alert_threshold:connections
  expr: sum by(tenant) (user_threshold{metric="connections"})
```

Service Discovery 透過 `prometheus.io/scrape: "true"` annotation 自動發現。

## 開發

```bash
# Build & load to Kind
make component-build COMP=threshold-exporter

# Deploy
make component-deploy COMP=threshold-exporter ENV=local

# Verify
make component-test COMP=threshold-exporter

# Scenario A end-to-end test
make test-scenario-a

# View metrics
curl http://localhost:8080/metrics | grep user_threshold

# View resolved config
curl http://localhost:8080/api/v1/config
```

## 修改閾值

```bash
# 方法 1: Helm upgrade (推薦)
helm upgrade threshold-exporter ./components/threshold-exporter \
  -n monitoring --set thresholdConfig.tenants.db-a.mysql_connections=50

# 方法 2: 直接 patch ConfigMap
kubectl edit configmap threshold-config -n monitoring
```

Exporter 會在 reload-interval 內自動載入新設定。
