#!/bin/bash
# ============================================================
# setup.sh — 部署環境到 Kind Cluster
# ============================================================
set -euo pipefail
source "$(cd "$(dirname "$0")" && pwd)/_lib.sh"

# 0. 重置模式
if [ "${1:-}" = "--reset" ]; then
  warn "Reset mode: Cleaning up old resources..."
  ./scripts/cleanup.sh
  sleep 3
fi

# 1. 檢查 Kind (Dev Container 應已預裝)
if ! command -v kind &>/dev/null; then
  err "Kind not found. Please ensure you are running in the Dev Container."
  exit 1
fi

if kind get clusters 2>/dev/null | grep -q "^${CLUSTER_NAME}$"; then
  log "Cluster '${CLUSTER_NAME}' exists."
else
  warn "Creating cluster '${CLUSTER_NAME}'..."
  kind create cluster --name "${CLUSTER_NAME}" --wait 60s
fi
ensure_kubeconfig

# 2. 部署 Namespaces
log "Creating namespaces..."
kubectl apply -f "${K8S_DIR}/00-namespaces/"

# 3. 部署 MariaDB (Helm)
HELM_DIR="${PROJECT_ROOT}/helm/mariadb-instance"
log "Deploying MariaDB instances..."
for inst in db-a db-b; do
  helm upgrade --install "mariadb-${inst}" "${HELM_DIR}" \
    -n "${inst}" -f "${PROJECT_ROOT}/helm/values-${inst}.yaml" --wait
  log "✓ ${inst} deployed"
done

# 4. 部署 Monitoring Stack
log "Deploying Monitoring Stack..."
kubectl apply -f "${K8S_DIR}/03-monitoring/"
kubectl rollout status deploy/prometheus -n monitoring --timeout=60s
kubectl rollout status deploy/kube-state-metrics -n monitoring --timeout=60s
log "✓ Monitoring stack deployed"

log "Setup complete! Run 'make status' to check."

# 5. 部署 Threshold Exporter (核心元件)
log "Building and Deploying Threshold Exporter..."

# 5.1 編譯最新影像檔
log "Building image: threshold-exporter:dev..."
docker build -t threshold-exporter:dev "${PROJECT_ROOT}/components/threshold-exporter/app"

# [新增] 5.1.5 將映像檔存成純 tar 檔
log "Exporting image to archive..."
docker save -o /tmp/threshold-exporter.tar threshold-exporter:dev

# 5.2 從 tar 檔載入到 Kind (這樣 Kind 就不會去比對 Docker 的清單了)
log "Loading image archive into Kind cluster..."
kind load image-archive /tmp/threshold-exporter.tar --name "${CLUSTER_NAME}"