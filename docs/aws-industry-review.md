# Dynamic Alerting Integrations — AWS & Industry Review Report

> **目的**: 從 AWS RDS / Monitoring 及業界做法的角度，評估本專案的技術方案是否值得投入。
> **背景**: 假設組織中 DB 平台團隊提供內部 RDS 服務，另一個 Monitoring 團隊提供監控服務。本報告聚焦技術價值判斷。

---

## Part 0: 最關鍵的問題 — 本專案的方案跟 AWS 一樣嗎？

### 直接回答：不一樣。這是兩種完全不同的架構哲學。

| | AWS CloudWatch 模型 | 本專案模型 (Configuration as Metrics) |
|---|---|---|
| **哲學** | 每個客戶自己建 Alarm，AWS 靠基礎設施暴力擴展 | 一條規則覆蓋所有租戶，靠 label 自動匹配 |
| **Alarm 數量** | O(n) — N 客戶 × M 指標 = N×M 個 Alarm | O(1) — 不論多少租戶，規則數量恆定 |
| **擴展方式** | Cell-based architecture + Shuffle Sharding（AWS 已公開此設計） | Prometheus label-based routing |
| **門檻值來源** | 硬寫在每個 Alarm 定義中 | 從 ConfigMap 轉為 metric，PromQL join 動態比較 |
| **新增租戶** | 建新 Alarm（AWS 自動化，客戶透過 Console/API 建） | 加一行 YAML 設定 |
| **營運者** | 客戶自管 | 平台團隊統一管理 |
| **商業模式** | 每個 Alarm $0.10/月（O(n) = 收入） | 邊際成本 $0 |

### AWS 為什麼能用 O(n) 模型？

因為 AWS 是**雲端服務提供商**，他們有：
- Cell-based architecture：每個 cell 自包含，alarm 分散到不同 cell 評估（[AWS Well-Architected](https://docs.aws.amazon.com/wellarchitected/latest/reducing-scope-of-impact-with-cell-based-architecture/faq.html)）
- Shuffle Sharding：4 instances/shard 可將故障影響縮小到 1/1680 的客戶（[AWS Builders Library](https://aws.amazon.com/builders-library/workload-isolation-using-shuffle-sharding)）
- 全球基礎設施：每個 Region 獨立的 CloudWatch 服務
- O(n) alarms 直接轉化為收入

**內部平台團隊不是 AWS，也不需要 AWS 等級的基礎設施。**

### 那本專案的方案像誰？

**Grafana Mimir/Cortex 的 overrides-exporter — 幾乎一模一樣。**

| | Cortex/Mimir overrides-exporter | 本專案 threshold-exporter |
|---|---|---|
| **功能** | 將 per-tenant limits 轉為 Prometheus metrics | 將 per-tenant thresholds 轉為 Prometheus metrics |
| **Metric 格式** | `cortex_limits_overrides{limit_name="...", user="..."}` | `user_threshold{tenant="...", metric="...", severity="..."}` |
| **配置來源** | `runtime.yaml`（支援 hot-reload） | ConfigMap（支援 hot-reload） |
| **規則數量** | O(1) rules + O(n) metric labels | O(1) rules + O(n) metric labels |

來源：[Cortex overrides-exporter](https://cortexmetrics.io/docs/guides/overrides-exporter/)、[Mimir overrides-exporter](https://grafana.com/docs/mimir/latest/references/architecture/components/overrides-exporter/)、[Cortex PR #3785](https://github.com/cortexproject/cortex/pull/3785)

**Google Borgmon（Prometheus 的前身）也是同樣理念。** Google SRE Book 記載：Borgmon 的 template libraries 就是「O(1) rules applied to O(n) services via labels」（[來源](https://sre.google/sre-book/practical-alerting/)）。

### 結論

> **本專案不是 AWS 的做法，也不需要是。**
> 本專案是 **Prometheus/Mimir/Borgmon 系譜** 的做法 — 這是 Google SRE 傳統下，針對**內部平台團隊**的正確架構選擇。
>
> 這不是 workaround，這是一個被 Grafana Mimir（業界頂級開源專案）驗證過的架構模式。

---

## Part 1: AWS RDS 原生監控能力 vs 本專案

### 1.1 AWS 為 RDS 提供什麼？

| AWS 服務 | 功能 | 對應本專案功能 |
|----------|------|--------------|
| **CloudWatch Metrics** | RDS 每 60s 發送 `CPUUtilization`、`DatabaseConnections` 等 | 對應 `mysqld-exporter` 的指標 |
| **Enhanced Monitoring** | OS 層級指標（1s 精度）| 資料送到 CloudWatch Logs，**不能直接建 Alarm**（已驗證） |
| **Performance Insights** | SQL 層級分析（Top SQL、Wait Events）| 本專案未涵蓋（AWS 加分項） |
| **CloudWatch Alarms** | 對指標設靜態門檻，觸發 SNS | 對應 Prometheus Alert Rules |
| **Composite Alarms** | AND/OR/NOT + AT_LEAST（2025/11 新增）| 對應 Scenario D |
| **Anomaly Detection** | ML-based 動態基線 | 本專案未實作（互補方案） |

### 1.2 CloudWatch Metric Math 的真實能力（已驗證）

**CloudWatch Alarm 的 threshold 參數必須是靜態數值** — 這是事實。

但 **Metric Math** 可以在 Alarm 內做運算式比較（例如 `m1 - m2 > 0`）。所以：

| 比較模式 | PromQL | CloudWatch |
|---------|--------|-----------|
| `metric_A > 80`（靜態門檻） | 可以 | 可以 |
| `metric_A > metric_B`（同 namespace 兩個 metric） | 可以 | **可以** — Metric Math `m1 - m2 > 0` |
| `metric_A > config{tenant="X"}`（per-tenant 動態門檻 + label 自動匹配） | 可以（`group_left`） | **不可行** — 無法用 label 自動匹配 |

**關鍵差距**：CloudWatch Metric Math 可以比較兩個已知 metric，但無法做 PromQL 的 `on(tenant) group_left` — 即自動按 tenant label 匹配門檻值。每個租戶仍需獨立 Alarm。

### 1.3 各 Scenario 在 CloudWatch 的可行性

| Scenario | CloudWatch 可行性 | 核心差距 |
|----------|-----------------|---------|
| **A: 動態門檻** | Metric Math 可做 metric 間運算，但**無法 per-tenant 自動匹配** | O(n) vs O(1) |
| **B: 弱環節偵測** | Container Insights Enhanced 有 container 指標，但 Metric Math 無法動態遍歷任意數量 container | 僵化 |
| **C: 狀態比對** | 有 `pod_container_status_waiting_reason_crash_loop_back_off` 指標（需 Enhanced Observability），但**無 per-tenant 開關** | 缺少開關 |
| **D: 複合優先權** | Composite Alarm + Suppressor 可做 AND/OR/抑制，但**每租戶需獨立 alarm 組合** | O(n) |

### 1.4 成本比較（已驗證定價，US East 2026）

| 規模 | 本專案 (Prometheus) | CloudWatch |
|------|-------------------|-----------|
| **50 租戶 × 4 指標** | 邊際 $0 | ~$45/月（$0.10×200 + $0.50×50） |
| **500 租戶 × 4 指標** | 邊際 $0 | ~$450/月 |
| **新增 1 租戶** | 加 ConfigMap entry，30 秒生效 | 建 4-5 Alarm + IaC 部署 |
| **基礎設施** | 需維護 Prometheus + Exporter | 全託管 |

> **註**：Prometheus 方式的「邊際 $0」前提是 Prometheus infra 已存在。

---

## Part 2: 業界怎麼做？

### 2.1 架構光譜

```
雲端服務商規模                              內部平台團隊規模
     ◄──────────────────────────────────────────────►

  AWS CloudWatch    Google Monarch    Grafana Mimir    本專案
  O(n) alarms       Standing Queries   overrides-exp.  threshold-exp.
  Cell-based        Zone-level eval    Hash ring shard  ConfigMap
  百萬級客戶         Google 內部         中大型 SaaS     數十~數百租戶
```

### 2.2 公司案例

| 公司 | 架構 | 與本專案的關係 |
|------|------|-------------|
| **AWS** | O(n) alarms + cell-based infra | 不同哲學 — 靠基礎設施暴力擴展 |
| **Google (Borgmon)** | O(1) template rules + label routing | **同一系譜** — Prometheus 的設計來源 |
| **Grafana Mimir** | overrides-exporter + ruler hash ring | **幾乎相同** — config → metric → O(1) rules |
| **Datadog** | 自建 Husky + Kafka per-tenant shard | 混合模式 — per-tenant 分片但自建評估引擎 |
| **Cloudflare** | 916 Prometheus × per-DC | Prometheus 生態但 per-DC 隔離 |

> **觀察**：沒有一家公司用 CloudWatch Alarms 做多租戶動態門檻。使用 Kubernetes 的公司都傾向 Prometheus 生態系。

### 2.3 Google SRE 告警最佳實踐

Google SRE Workbook 推薦 Multi-Window, Multi-Burn-Rate 告警（[來源](https://sre.google/workbook/alerting-on-slos/)）。
本專案的多層級嚴重度（warning + critical + `unless` 降級）方向一致。

### 2.4 告警疲勞數據（[incident.io, 2025](https://incident.io/blog/alert-fatigue-solutions-for-dev-ops-teams-in-2025-what-works)）

- 團隊平均每週 **2,000+ 告警**，僅 **3% 需要立即處理**
- **85%** 是誤報
- 本專案 Scenario D 的維護抑制、複合告警、多層級嚴重度是業界推薦的解法

---

## Part 3: 什麼時候需要本方案？什麼時候不需要？

### 3.1 「per-tenant 動態門檻需求」是什麼？

簡單說：**你的 DB 使用者是否需要設定不同的告警門檻？**

| 場景 | 範例 | 需要本方案？ |
|------|------|:-----------:|
| 所有 DB 用相同門檻 | 全部 CPU > 80% 就告警 | 不需要 |
| 不同 DB 要不同門檻 | Team A 要 70%，Team B 要 90% | **需要** |
| 有些 DB 要關閉特定告警 | Team C 不想收 CPU 告警 | **需要** |
| 維護時要自動抑制告警 | Team A 週六維護，不要告警 | **需要** |
| 同一指標要 warning + critical | 70% warning，90% critical | **需要** |

### 3.2 如果 Monitoring Team 像 CloudWatch 一樣呢？

如果 Monitoring Team 為每個 DB 使用者建獨立 Alarm（O(n) 模式），**技術上可行**。但：

| | Monitoring Team 的 O(n) 方式 | 本專案的 O(1) 方式 |
|---|---|---|
| DB 使用者自訂門檻 | 使用者提需求 → Monitoring Team 建/改 Alarm | 使用者改 ConfigMap → 30 秒自動生效 |
| 新增一個 DB | Monitoring Team 建 4-5 個新 Alarm | 加一行 YAML |
| 門檻修改速度 | 走需求/部署流程 | 即時（30 秒） |
| 維護負擔歸誰 | Monitoring Team | DB Platform Team |
| 規模問題 | 500 DB × 5 指標 = 2500 Alarm 管理 | 規則恆定 < 10 條 |

**結論**：如果 Monitoring Team 願意管理 2500 個 Alarm 且使用者接受走需求流程改門檻，那不需要本方案。但如果使用者需要**自助式**即時調整門檻，本方案解決了這個問題。

### 3.3 DB 使用者要自訂告警怎麼做？

本專案提供的使用者流程：

```
DB 使用者想要：CPU > 70% 就告警

操作方式（三選一）：
1. 改 YAML ConfigMap：mysql_cpu: "70"     → 30 秒自動生效
2. 用 patch 工具：patch_cm.py db-a mysql_cpu 70  → 30 秒自動生效
3. 未來可接 API/UI（ConfigMap 是資料來源，可擴展為 API → DB → Exporter）

使用者想要：不想收 CPU 告警
操作方式：
  patch_cm.py db-a mysql_cpu disable     → 該指標不再產生 metric → alert rule 自然不觸發

使用者想要：恢復預設
操作方式：
  patch_cm.py db-a mysql_cpu default     → 刪除自訂值，使用 defaults 區段的值
```

---

## Part 4: 有現成的開源可以用嗎？

### 直接回答：沒有。

已逐一驗證所有候選方案：

| 開源方案 | 能否直接替換？ | 原因 |
|---------|:-----------:|------|
| **Mimir overrides-exporter** | 不能 | 只暴露 Mimir **內部** limit（ingestion_rate、max_series 等），**不支援自訂閾值**。被鎖定在 Mimir 的 struct 定義 |
| **Cortex overrides-exporter** | 不能 | 同上，被鎖定在 Cortex 的 `limit_config` struct |
| **json_exporter** | 不能 | 只支援 remote HTTP JSON，**不能讀本地 YAML**，無三態邏輯 |
| **Pushgateway** | 不建議 | Prometheus **官方標記為 anti-pattern**，stale data 風險 |
| **Grafana Unified Alerting** | 部分 | 可做動態門檻比較，但**仍然需要**一個 threshold datasource（也就是仍需 exporter） |
| **Robusta / Komodor** | 不能 | 在 alert 下游做通知路由，不定義門檻 |

### Mimir overrides-exporter 為什麼不能用？

Mimir overrides-exporter 的**概念**跟 threshold-exporter 一樣（config → metric），但它只暴露 Mimir 自己定義的內部 limit，例如：
```
cortex_limits_overrides{limit_name="ingestion_rate", user="tenant-1"} 25000
cortex_limits_overrides{limit_name="max_global_series_per_user", user="tenant-1"} 150000
```
你不能加入 `mysql_connections: 70` 這種自訂閾值。它的 struct 是硬寫的。

### threshold-exporter 有多大？

**~330 行 Go code**（main.go + config.go + collector.go）。這個維護成本非常低。比起引入一個不完全匹配的開源工具再做大量 wrapper，直接維護自己的 330 行 code 更實際。

---

## Part 5: 挑戰 — 如果目標是「建內部 AWS」，這專案還值得嗎？

### 5.1 如果 DB Platform Team 是 RDS team、Monitoring Team 是 CloudWatch team

在 AWS 的模型中，分工是這樣的：

| 角色 | AWS 的做法 | 組織內的對應 |
|------|----------|-------------|
| **RDS Team** | 提供 DB 實例 + 暴露 metrics（CPUUtilization 等）| DB Platform Team：提供內部 RDS + mysqld-exporter |
| **CloudWatch Team** | 提供 alarm 基礎設施，使用者自助建 alarm | Monitoring Team：提供？？？ |
| **使用者** | 透過 Console/API 自己建 alarm，設定任意門檻 | DB Users：怎麼設定？ |

**在 AWS 模型中，RDS team 不會建自己的告警系統。** CloudWatch 才是告警系統。RDS team 的職責是：
1. 暴露 metrics（做好 mysqld-exporter 這類工作）
2. 提供 golden metrics 建議（例如「建議監控 CPUUtilization > 80%」）
3. 提供 Performance Insights、Enhanced Monitoring 等加值服務

**所以誠實的回答是：如果目標是「建內部 AWS」，這個專案的定位有問題。**

### 5.2 問題出在哪？

問題不在技術，在**組織分工**：

| 場景 | 需要本專案？ |
|------|:-----------:|
| Monitoring Team 提供完整自助 alarm 平台（使用者可即時建/改/刪 alarm） | **不需要** — 像 CloudWatch 一樣讓使用者自己管理 |
| Monitoring Team 只提供 golden alerts，使用者無法自訂 | **需要** — 但應該是推動 Monitoring Team 提供自訂功能 |
| Monitoring Team 有計畫提供自訂功能但還沒做好 | **暫時需要** — 作為 bridge solution |
| Monitoring Team 完全不打算支援自訂門檻 | **需要** — workaround |

### 5.3 使用者想自訂 golden AP alert 沒有的指標怎麼辦？

**這是本專案最大的限制。**

在 AWS CloudWatch 模型中：
- 使用者可以對**任何** CloudWatch metric 建 alarm
- 想監控 `DatabaseConnections > 100`？直接建 alarm
- 想監控 `ReadLatency > 50ms`？也可以
- 不需要 RDS team 事先定義哪些指標「可告警」

在本專案的 Configuration as Metrics 模型中：
- **使用者只能調整 exporter 預先定義的指標門檻**
- 目前支援：`mysql_connections`、`mysql_cpu`、`container_cpu`、`container_memory`
- 如果使用者想監控 `ReadLatency`（不在列表中）→ **不行**，除非修改 exporter code 加入新指標
- 如果使用者想監控完全自訂的 PromQL → **不行**

**對比：**

| 能力 | AWS CloudWatch | 本專案 |
|------|:---:|:---:|
| 對預定義指標設門檻 | 可以 | 可以 |
| 對任意指標設門檻 | **可以** | **不可以** — 需改 code |
| 使用者完全自訂告警邏輯 | **可以** — 任意 metric + 任意 threshold | **不可以** — 只能調整預定義的指標 |
| 新增可監控指標 | 不需要改 code | **需要改 exporter code** |

### 5.4 真正的決策點

核心決策點：

**問組織的 Monitoring 團隊一個問題：「DB 使用者能否自助建立/修改告警，設定自己想要的門檻值？」**

- **如果答案是「可以」** → 不需要本專案。讓使用者用 Monitoring Team 的平台自己管理，像 CloudWatch 使用者一樣。
- **如果答案是「不可以」** → 分兩種情況：
  - Monitoring Team 有計畫做 → 本專案是 **bridge solution**，等做好就可以退役
  - Monitoring Team 不打算做 → 本專案是 **permanent workaround**，但有上述限制（只能調預定義指標）

### 5.5 如果要走「內部 AWS」路線，RDS team 該做什麼？

| 職責 | 做法 |
|------|------|
| **暴露 metrics** | 維護 mysqld-exporter，確保所有重要的 MySQL metrics 都有被 scrape |
| **提供 golden alerts 建議** | 提供一組預設告警規則（如 CPU > 80%, Connections > 80%），但不強制 |
| **推動 Monitoring Team** | 要求 Monitoring Team 提供 per-tenant 自助告警平台 |
| **提供加值服務** | Performance Insights 等效功能、慢查詢分析、健康報告 |
| **不做的事** | 不自建告警系統 — 這是 Monitoring Team 的職責 |

---

## Part 6: 值不值得做？（最終結論）

### 誠實的評估

| 面向 | 評估 |
|------|------|
| **技術可行性** | 已驗證，4 個 Scenario 都能運作 |
| **業界認可度** | Configuration as Metrics 是 Prometheus 生態的已知模式 |
| **OSS 替代** | 沒有現成替代品 |
| **自建成本** | ~330 行 Go code，極低 |
| **限制** | 使用者只能調整預定義指標，無法自訂任意告警 |
| **組織定位** | 如果目標是「內部 AWS」，告警系統應是 Monitoring Team 的職責 |

### 三種可能的定位

**A. 作為 POC / 需求規格（最推薦）**
> 本專案已經驗證了「per-tenant 動態門檻」的技術可行性。用這個成果作為需求規格，推動 Monitoring Team 在平台上支援類似功能。這是最符合「內部 AWS」分工的做法。

**B. 作為 Bridge Solution（次推薦）**
> 如果 Monitoring Team 暫時無法支援自訂門檻，先用本專案頂著。等 Monitoring Team 的方案就緒後退役。但要接受限制：使用者只能調整預定義指標。

**C. 作為 Permanent Solution（有條件）**
> 如果 Monitoring Team 確認不打算支援 per-tenant 自訂，且團隊願意長期維護，可以作為永久方案。但需要擴展 exporter 支援更多指標，並考慮建立 API/UI 讓使用者操作。

---

## Part 7: 一頁總結

**本專案的方案跟 AWS 一樣嗎？** 不一樣。AWS 用 O(n) alarms（CloudWatch），本專案用 O(1) rules（Configuration as Metrics）。

**在「內部 AWS」模型中，這是誰的工作？** 告警系統是 Monitoring Team（CloudWatch team）的職責。RDS team 的職責是暴露 metrics + 提供 golden alerts 建議。

**那這專案沒用嗎？** 不是。它有三個可能的價值：
1. **最大價值**：作為 POC 成果和需求規格，推動 Monitoring Team 支援 per-tenant 自訂門檻
2. **次要價值**：作為 bridge solution，在 Monitoring Team 方案就緒前頂著用
3. **限制**：使用者只能調預定義指標，想自訂不在列表中的告警（如 ReadLatency）→ 需改 exporter code

**有現成 OSS 嗎？** 沒有。已驗證 Mimir/Cortex/json_exporter/Pushgateway/Grafana/Robusta。

**使用者想自訂 golden AP alert 沒有的指標怎麼辦？** 本專案無法支援。使用者只能調整 exporter 預定義的指標門檻。這是與 CloudWatch（可對任意 metric 建 alarm）的根本差距。

**建議**：用本專案的 POC 成果，和 Monitoring Team 談「平台能否支援 per-tenant 自訂門檻？」如果能 → 用他們的；如果不能 → 本專案是唯一的替代方案。

---

## 附錄 A: Prometheus 通用擴展限制

### A.1 單一 Prometheus Instance 的硬限制

| 維度 | 安全範圍 | 開始需注意 | 需要水平擴展 |
|------|---------|-----------|-------------|
| **Active Time Series** | < 1M | 1-5M | > 10M |
| **RAM** | 4-8 GB | 16-32 GB | > 64 GB |
| **Recording Rules** | < 500 | 500-2,000 | > 5,000 |
| **Alert Rules** | < 200 | 200-1,000 | > 2,000 |
| **Scrape Targets** | < 500 | 500-2,000 | > 5,000 |
| **Ingestion Rate** | < 100K samples/s | 100-500K | > 500K samples/s |

### A.2 Time Series 記憶體消耗

每條 time series 在 Prometheus 記憶體中的佔用：

| 來源 | 估算值 | 公式 |
|------|--------|------|
| Robust Perception（Brian Brazil） | ~4 KB/series | 732 bytes 基礎 + 32 bytes/label × N + Go GC 翻倍 |
| Cloudflare 實測 | ~4 KB/series | `go_memstats_alloc_bytes / prometheus_tsdb_head_series` |
| Prometheus 維護者經驗法則 | ~4.5 GB / 1M series | cardinality ~2 GiB + ingestion ~2.5 GiB |

**Series 數量 vs 記憶體估算：**

| Active Series | 估算 RAM | 實際案例 |
|:---:|:---:|------|
| 100K | ~0.5 GB | 小型部署 |
| 1M | ~4-5 GB | 常見生產環境 |
| 5M | ~20 GB | Cloudflare 平均值 |
| 10M | ~40-60 GB | 需高配 instance |
| 30M | ~120 GB | Cloudflare 最大 instance |

### A.3 Alert / Recording Rule 數量限制

**Prometheus 沒有硬性的 alert rule 數量上限。** 限制是「所有 rules 能否在 evaluation interval（預設 15s）內完成」。

| 數量 | 可行性 | 參考 |
|------|--------|------|
| **< 100 rules** | 完全無壓力 | kube-prometheus 預設 86 條，僅增 ~1% CPU |
| **100-1,000 rules** | 正常生產環境 | 常見 |
| **1,000-5,000 rules** | 可行，需合理硬體 | — |
| **5,000-18,000 rules** | 有客戶實際在用 | [Grafana Labs](https://grafana.com/blog/2021/08/04/how-to-use-promql-joins-for-more-effective-queries-of-prometheus-metrics-at-scale/) |
| **> 20,000 rules** | 需 Mimir Ruler（hash ring 分片執行） | — |

**實際瓶頸不是數量，是每條 rule 的查詢時間：**
- `rate(metric[5m])` → 快（毫秒級）
- `rate(metric[30d])` → 慢（可能秒級）
- `group_left` join on 大量 series → 取決於兩側 series 數
- 任一 rule 查詢時間 > evaluation interval → **跳過該輪**

**執行模型：**
- Rule groups 之間**平行**執行
- 同一 group 內**循序**執行（v2.44+ 可開啟 `concurrent-rule-eval` feature flag 做並行）
- `group_left` 用 `on(label)` 精確匹配時，Prometheus 用 hash map 優化為 O(N+M)

**監控 rule evaluation 健康：**
```promql
prometheus_rule_group_iterations_missed_total    # > 0 表示有跳過
prometheus_rule_evaluation_duration_seconds       # 每個 group 的評估時間
prometheus_rule_group_last_duration_seconds        # 最近一次評估耗時
```

**來源**：[Grafana Blog](https://grafana.com/blog/2021/08/04/how-to-use-promql-joins-for-more-effective-queries-of-prometheus-metrics-at-scale/)、[Prometheus Feature Flags](https://prometheus.io/docs/prometheus/latest/feature_flags/)、[Rule Evaluation Fix](https://povilasv.me/how-to-fix-prometheus-missing-rule-evaluations/)

### A.4 Cardinality 是最大殺手

**超過限制時的症狀：**
1. **OOM Kill** — K8s 中最常見
2. **查詢報錯** — `"query processing would load too many samples into memory"`
3. **Ingestion 節流** — scrape 超時、資料遺失
4. **WAL replay 過慢** — 重啟時間從秒級變成分鐘/小時級

**Cardinality 爆炸的常見原因：**
- label 值是 user ID / request ID / IP address 等高基數欄位
- 動態 label 值不斷新增但舊的 series 不會被釋放（churn）
- 一個 metric 搭配多個高基數 label 的組合爆炸

### A.5 真實世界參考點

| 案例 | Series 數 | 來源 |
|------|---------|------|
| **Cloudflare** | 916 instances × ~5M avg = **49 億總計** | [Blog](https://blog.cloudflare.com/how-cloudflare-runs-prometheus-at-scale/) |
| **Prometheus 維護者建議** | 單一 instance "a few million" | [GitHub #5579](https://github.com/prometheus/prometheus/issues/5579) |
| **AWS AMP 預設上限** | 50M/workspace | [AWS](https://aws.amazon.com/about-aws/whats-new/2025/07/amazon-managed-service-prometheus-50M-default-activeserieslimit/) |
| **Grafana Mimir 測試** | 1B active series（單 tenant） | [GitHub Discussion](https://github.com/grafana/mimir/discussions/3380) |

### A.6 何時需要水平擴展？

| 方案 | 適合場景 | Series 上限 |
|------|---------|:---:|
| **單一 Prometheus** | < 5M series | ~10M（tuned） |
| **Thanos** | 跨 instance 全局查詢 + 長期儲存 | 數十億（加總） |
| **Grafana Mimir** | 企業級多租戶 | 已測 1B/tenant |
| **VictoriaMetrics** | 高效能替代 | 更好的記憶體效率 |

Thanos 對既有 Prometheus 的遷移最平滑（加 sidecar 即可）。

### A.7 K8s ConfigMap 限制

- etcd 限制單一 ConfigMap **~1 MiB**
- `last-applied-configuration` annotation 也佔空間，實際可用 **~0.5 MB**
- prometheus-operator 超過 1MiB 時會自動拆分 ConfigMaps
- Prometheus YAML 本身沒有檔案大小限制，可拆成多個 rule_files

---

## 附錄 B: 已驗證的 AWS 技術聲明

所有關鍵聲明已對照 AWS 官方文件驗證：

| 聲明 | 結果 | 來源 |
|------|------|------|
| CW Alarm threshold 必須靜態 | **部分正確** — threshold 靜態，Metric Math 可間接比較。仍無法 per-tenant 自動匹配 | [Metric Math](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Create-alarm-on-metric-math-expression.html) |
| Composite Alarm 支援 AND/OR/NOT | **正確** + 2025/11 AT_LEAST | [Composite Alarms](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Create_Composite_Alarm.html) |
| Standard Alarm $0.10/月 | **正確** (US East) | [Pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| AMP 支援標準 PromQL rules | **正確** | [AMP Ruler](https://docs.aws.amazon.com/prometheus/latest/userguide/AMP-Ruler.html) |
| Enhanced Monitoring 送 Logs 不送 Metrics | **正確** | [Enhanced Monitoring](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_Monitoring.OS.html) |
| AT_LEAST 函數 2025/11 發布 | **正確** (2025-11-11) | [What's New](https://aws.amazon.com/about-aws/whats-new/2025/11/amazon-cloudwatch-composite-alarms-threshold-based/) |
| Anomaly Detection $0.30/月 | **正確** (3×$0.10) | [Pricing](https://aws.amazon.com/cloudwatch/pricing/) |
| Container Insights CrashLoop metric | **正確** (需 Enhanced Observability) | [CI Enhanced](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Container-Insights-metrics-enhanced-EKS.html) |

### 其他參考
- [AWS Shuffle Sharding](https://aws.amazon.com/builders-library/workload-isolation-using-shuffle-sharding)
- [Cortex overrides-exporter](https://cortexmetrics.io/docs/guides/overrides-exporter/)
- [Mimir overrides-exporter](https://grafana.com/docs/mimir/latest/references/architecture/components/overrides-exporter/)
- [Cortex PR #3785: overrides-exporter 動機](https://github.com/cortexproject/cortex/pull/3785)
- [Google SRE Book: Borgmon](https://sre.google/sre-book/practical-alerting/)
- [Cloudflare Prometheus at Scale](https://blog.cloudflare.com/how-cloudflare-runs-prometheus-at-scale/)
- [Datadog Husky](https://www.datadoghq.com/blog/engineering/husky-deep-dive/)
- [Prometheus Pushgateway: When to use](https://prometheus.io/docs/practices/pushing/)
- [json_exporter](https://github.com/prometheus-community/json_exporter)
- [Grafana Dynamic Thresholds](https://grafana.com/docs/grafana/latest/alerting/examples/dynamic-thresholds/)
