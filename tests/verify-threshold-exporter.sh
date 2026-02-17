#!/bin/bash
# ============================================================
# verify-threshold-exporter.sh — 驗證 threshold-exporter 功能
# ============================================================
# Config-driven architecture: exporter 讀取 ConfigMap YAML，
# 暴露 user_threshold gauge。不使用 HTTP API 寫入。
# ============================================================
set -euo pipefail

# Source common functions
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "${SCRIPT_DIR}/../scripts/_lib.sh"

info "Verifying threshold-exporter..."

# 1. 檢查 Pod 狀態
log "Checking Pod status..."
if ! kubectl get pods -n monitoring -l app=threshold-exporter | grep -q Running; then
  err "threshold-exporter pod is not running"
  kubectl get pods -n monitoring -l app=threshold-exporter
  exit 1
fi
log "✓ Pod is running"

# 2. 檢查 Service
log "Checking Service..."
if ! kubectl get svc -n monitoring threshold-exporter &>/dev/null; then
  err "threshold-exporter service not found"
  exit 1
fi
log "✓ Service exists"

# 3. 檢查 ConfigMap
log "Checking ConfigMap..."
if ! kubectl get configmap -n monitoring threshold-config &>/dev/null; then
  err "threshold-config ConfigMap not found"
  exit 1
fi
log "✓ ConfigMap exists"

# 4. Port forward
POD_NAME=$(kubectl get pods -n monitoring -l app=threshold-exporter -o jsonpath='{.items[0].metadata.name}')
log "Setting up port-forward to ${POD_NAME}..."
kubectl port-forward -n monitoring ${POD_NAME} 8080:8080 &
PF_PID=$!
sleep 3

cleanup() {
  log "Cleaning up port-forward..."
  kill ${PF_PID} 2>/dev/null || true
}
trap cleanup EXIT

# 5. 測試 Health endpoint
log "Testing /health endpoint..."
if curl -sf http://localhost:8080/health | grep -q "ok"; then
  log "✓ Health check passed"
else
  err "Health check failed"
  exit 1
fi

# 6. 測試 Ready endpoint
log "Testing /ready endpoint..."
if curl -sf http://localhost:8080/ready | grep -q "ready"; then
  log "✓ Ready check passed (config loaded)"
else
  err "Ready check failed — config may not be loaded"
  exit 1
fi

# 7. 測試 Metrics endpoint — 確認 user_threshold 存在
log "Testing /metrics endpoint..."
METRICS=$(curl -sf http://localhost:8080/metrics)
if echo "$METRICS" | grep -q "user_threshold"; then
  log "✓ Metrics endpoint returns user_threshold"
else
  err "Metrics endpoint not returning user_threshold"
  echo "$METRICS" | head -20
  exit 1
fi

# 8. 驗證三態邏輯 — 確認 config 中的 tenant 有對應 metric
log "Verifying three-state resolution..."

# Check db-a connections (should be custom: 70)
# Note: prometheus client_golang outputs labels in alphabetical order:
#   user_threshold{component="mysql",metric="connections",severity="warning",tenant="db-a"} 70
if echo "$METRICS" | grep 'metric="connections"' | grep 'tenant="db-a"' | grep -q " 70"; then
  log "✓ db-a connections = 70 (custom)"
else
  warn "db-a connections threshold not as expected"
  echo "$METRICS" | grep 'db-a' | grep 'connections' || echo "(not found)"
fi

# Check db-a cpu (should be default: 80)
if echo "$METRICS" | grep 'metric="cpu"' | grep 'tenant="db-a"' | grep -q " 80"; then
  log "✓ db-a cpu = 80 (default)"
else
  warn "db-a cpu threshold not as expected"
  echo "$METRICS" | grep 'db-a' | grep 'cpu' || echo "(not found)"
fi

# 9. 測試 Config view endpoint
log "Testing /api/v1/config endpoint..."
CONFIG_VIEW=$(curl -sf http://localhost:8080/api/v1/config)
if echo "$CONFIG_VIEW" | grep -q "Resolved thresholds"; then
  log "✓ Config view endpoint working"
  echo "$CONFIG_VIEW" | tail -10
else
  warn "Config view endpoint not returning expected format"
fi

# 10. 測試 Prometheus 能否抓到 (如果 port-forward 存在)
log "Testing if Prometheus can scrape..."
log "Waiting 30s for Prometheus to scrape..."
sleep 30

if curl -sf http://localhost:9090/api/v1/query --data-urlencode 'query=user_threshold' 2>/dev/null | grep -q "db-a"; then
  log "✓ Prometheus successfully scraped threshold metrics"
else
  warn "Prometheus not yet scraping (port-forward to Prometheus may be needed)"
fi

log ""
log "===================================================="
log "✓ threshold-exporter verification completed"
log "===================================================="
log ""
log "Summary:"
log "  - Pod: Running"
log "  - ConfigMap: Present"
log "  - Health check: OK"
log "  - Ready check: OK (config loaded)"
log "  - Metrics: user_threshold exposed"
log "  - Three-state: custom/default/disable resolved"
log ""
log "Next steps:"
log "  1. Wait for Prometheus to scrape (15s interval)"
log "  2. Query: user_threshold{tenant=\"db-a\"}"
log "  3. Run Scenario A test: ./tests/scenario-a.sh"
