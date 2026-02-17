#!/usr/bin/env python3
import sys
import yaml # 需要 pip install pyyaml
import subprocess
import json
import argparse

# 設定目標
NAMESPACE = "monitoring"
CONFIGMAP_NAME = "threshold-config"
KEY_IN_CM = "config.yaml"

def get_current_config():
    """從 K8s 獲取當前的 ConfigMap YAML"""
    try:
        # 使用 kubectl 抓取特定的 key
        cmd = ["kubectl", "get", "cm", CONFIGMAP_NAME, "-n", NAMESPACE, "-o", "jsonpath={.data['config\\.yaml']}"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        # 解析 YAML
        return yaml.safe_load(result.stdout)
    except subprocess.CalledProcessError as e:
        print(f"Error fetching ConfigMap: {e.stderr}", file=sys.stderr)
        sys.exit(1)

def apply_config(new_config_data):
    """將修改後的 Config 寫回 K8s"""
    # 轉回 YAML 字串 (不使用 flow style 以保持可讀性)
    yaml_str = yaml.dump(new_config_data, default_flow_style=False, sort_keys=False)
    
    # 建構 Patch JSON (只更新 data 部分)
    patch_data = {
        "data": {
            KEY_IN_CM: yaml_str
        }
    }
    
    try:
        # 使用 kubectl patch 進行原子更新
        cmd = ["kubectl", "patch", "cm", CONFIGMAP_NAME, "-n", NAMESPACE, "--type=merge", "-p", json.dumps(patch_data)]
        subprocess.run(cmd, check=True, stdout=subprocess.DEVNULL)
        print(f"✓ ConfigMap '{CONFIGMAP_NAME}' updated successfully.")
    except subprocess.CalledProcessError as e:
        print(f"Error patching ConfigMap: {e}", file=sys.stderr)
        sys.exit(1)

def update_tenant(config, tenant, metric, value, priority=None):
    """修改記憶體中的 Config 物件"""
    # 確保結構存在
    if "tenants" not in config:
        config["tenants"] = {}
    
    if tenant not in config["tenants"]:
        config["tenants"][tenant] = {}
    
    # 邏輯 1: 恢復預設值 (移除 override)
    if value.lower() == "default":
        if metric in config["tenants"][tenant]:
            del config["tenants"][tenant][metric]
            print(f"  Action: Removing override for {tenant}.{metric} -> Revert to Default")
        else:
            print(f"  Action: {tenant}.{metric} is already default.")
    
    # 邏輯 2: 設定數值 (含優先級)
    else:
        final_val = value
        # Week 4 Scenario D: 支援 "90:high" 格式
        if priority:
            final_val = f"{value}:{priority}"
            
        config["tenants"][tenant][metric] = final_val
        print(f"  Action: Set {tenant}.{metric} = {final_val}")

def main():
    # 定義指令參數
    parser = argparse.ArgumentParser(description="Patch threshold-config ConfigMap safely.")
    parser.add_argument("tenant", help="Target tenant (e.g., db-a)")
    parser.add_argument("metric", help="Metric key (e.g., mysql_connections, container_cpu)")
    parser.add_argument("value", help="Value (number, 'disable', or 'default')")
    parser.add_argument("--priority", help="Optional priority tag (e.g., high, critical)")
    
    args = parser.parse_args()

    # 執行流程
    print(f"Patching {NAMESPACE}/{CONFIGMAP_NAME}...")
    try:
        config = get_current_config()
        update_tenant(config, args.tenant, args.metric, args.value, args.priority)
        apply_config(config)
    except Exception as e:
        print(f"Unexpected error: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()