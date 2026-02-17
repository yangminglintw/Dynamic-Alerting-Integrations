# Skill: update-config

## Description
動態修改 `monitoring/threshold-config` ConfigMap。
使用 Python `yaml` 庫確保格式正確。
支援 **Week 4 Scenario D** 的優先級 (Priority) 標籤。

## Usage
當需要調整閾值、停用監控或設定優先級時使用。

## Interface
```bash
python3 .claude/skills/update-config/scripts/patch_cm.py <tenant> <metric> <value> [--priority <prio>]