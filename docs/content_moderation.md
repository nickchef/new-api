# Content Moderation 模块

## 概述

new-api 内置两层串联的内容审核体系：

```
┌─────────────────────────────────────────────────────────────┐
│ relay 路由组                                                 │
│  └─ middleware.ContentModeration()  ← 在 controller 之前      │
│       ├─ L1: 本地 AC 词库 (service/sensitive.go)             │
│       └─ L2: OpenAI omni-moderation-latest (按 mode 工作)     │
└─────────────────────────────────────────────────────────────┘
```

- **L1（本地词库）**：保留 new-api 原有 `service.CheckSensitiveText` 不变，始终同步硬拦。
- **L2（OpenAI Moderation）**：新增 `ContentModerationService`，支持 `off` / `observe` / `pre_block` 三种工作模式。

任意一层命中即按请求协议（OpenAI / Claude / Gemini）格式返回错误。`Enabled=false` 时主链路零开销。

## 启用步骤

1. 登录 admin 控制台，打开"系统设置 → 内容审核"。
2. 填写 OpenAI Moderation API Key（支持多 Key 轮转）。
3. 点击"测试连通性"确认 Key 可用。
4. 选择 `observe` 模式跑 24-48h 观察日志。
5. 调整 13 类阈值（默认值见 `setting/content_moderation.go::ContentModerationDefaultThresholds`）。
6. 切换到 `pre_block` 模式正式拦截。

## 配置项说明

| 字段 | 默认值 | 说明 | 调优建议 |
|------|--------|------|----------|
| `enabled` | false | 模块总开关 | 灰度初期保持 false，跑完 observe 再开 |
| `mode` | off | off / observe / pre_block | 见上文 |
| `base_url` | https://api.openai.com | OpenAI 兼容端点 | 可指向第三方代理 |
| `model` | omni-moderation-latest | 审核模型 | 不建议改 |
| `api_keys` | [] | Key 列表，多个则轮转 | 至少 2 个保活 |
| `timeout_ms` | 3000 | 单次调用超时 | 1000-10000，太长会拖慢主链路 |
| `retry_count` | 1 | 5xx 重试次数 | 0-3 |
| `thresholds` | 13 类，0.65-0.98 | 各类别拦截阈值 | 越低越严 |
| `sample_rate` | 100 | 采样百分比 | observe 阶段可降至 10 验证延迟影响 |
| `input_scope` | last_user | 审核范围 | last_user / all_user / all_messages |
| `pre_hash_check_enabled` | true | Hash 黑名单短路 | 关闭仅在调试时使用 |
| `model_mode` | all | all / whitelist / blacklist | 选择性审核特定模型 |
| `model_list` | [] | 通配符模式列表 | 例：`gpt-4*`、`*claude*` |
| `block_status` | 403 | OpenAI 协议拦截 HTTP 状态码 | Claude/Gemini 固定 400 |
| `block_message` | 默认 i18n | 拦截响应正文 | 走 i18n key `content_moderation.blocked` |
| `auto_ban_enabled` | true | 是否启用自动封禁 | 关闭则仅记录违规计数 |
| `ban_threshold` | 10 | 窗口内违规阈值 | 视用户量调整 |
| `violation_window_hours` | 720 | 违规计数滑窗 | 默认 30 天 |
| `email_on_hit` | true | 命中时发送邮件 | 24h 限频，不会刷屏 |
| `email_to_admin` / `email_to_user` | true/false | 邮件分别发给谁 | |
| `worker_count` | 4 | observe 模式 worker 数 | 上限 32，按 QPS 调整 |
| `queue_size` | 32768 | observe 入队缓冲 | 满则 drop |
| `record_non_hits` | false | 未命中是否落库 | 仅 observe 调阈值时开 |
| `hit_retention_days` | 180 | 命中日志保留 | 法务/审计需求 |
| `non_hit_retention_days` | 3 | 未命中日志保留 | 数据量大可设 1 |

## 灰度上线 SOP

1. **Day 0**：`mode=off`，部署上线，验证主链路 P99 延迟不受影响（应与基线相差 ≤5%）。
2. **Day 1-2**：`mode=observe`，`sample_rate=10`，跑 24h，看日志命中率和分类分布。
3. **Day 3**：根据 observe 日志，调整 13 类阈值（默认偏宽松，可适度收紧 hate/sexual/violence）。
4. **Day 4**：提升 `sample_rate=100`，观察 worker queue 长度是否堆积。若 dropped > 0%，提升 `worker_count`。
5. **Day 5**：`mode=pre_block`，先在 1 个频道上灰度，观察 24h。
6. **Day 6**：全量。

## 回滚预案

任何阶段都可立即设 `enabled=false` 或 `mode=off`，主链路立即恢复零开销。

若需要保留指标但临时关闭拦截：`mode=observe` + `sample_rate=0`，等于完全短路 L2。

## 13 类阈值调优指南

| 类别 | 默认 | 收紧建议 | 放宽建议 |
|------|------|----------|----------|
| harassment | 0.98 | 一般用户向应用慎收紧 | 内部工具可设 0.7 |
| harassment/threatening | 0.90 | 客服类应用建议 0.75 | |
| hate | 0.65 | 已较严 | 可放至 0.85 |
| hate/threatening | 0.65 | 已较严 | |
| illicit | 0.95 | 教育/医疗场景 0.7 | |
| illicit/violent | 0.95 | 同上 | |
| self-harm | 0.65 | 心理健康场景 0.5 | |
| self-harm/intent | 0.85 | 同上 | |
| self-harm/instructions | 0.65 | 同上 | |
| sexual | 0.65 | C 端应用 0.55 | 18+ 内容创作 0.95 |
| sexual/minors | 0.65 | 一律不放宽 | 不放宽 |
| violence | 0.95 | 游戏/小说创作可 0.99 | |
| violence/graphic | 0.95 | 新闻类 0.99 | |

## 模型选择性审核示例

```json
{
  "model_mode": "whitelist",
  "model_list": ["gpt-4*", "claude-3-*"]
}
```

只对 GPT-4 系列和 Claude 3 系列做审核，其它模型（如 Embedding / 自定义模型）跳过。

```json
{
  "model_mode": "blacklist",
  "model_list": ["embedding-*", "*-vision*"]
}
```

跳过 embedding 类和带 vision 的模型，其它都审核。

## FAQ

### Q: Redis 不可达会怎样？
- Hash 预检降级：每次都走 OpenAI（不会 fail-close）。
- 违规计数器降级：使用进程内存（单实例可用；多实例下不再准确）。
- 邮件限频降级：fail-open，可能短时间多发几封。
- 主链路审核：不受影响，OpenAI 调用正常。

### Q: OpenAI Moderation 调用失败怎么办？
- 单次失败：自动重试 1 次。
- 单 Key 3 次失败：冻结 60s。
- 全部 Key 都冻结：fail-open，主链路放行 + 写 error 日志。

### Q: 误判怎么修正？
1. admin 控制台 → 黑名单 → 输入 hash 删除。
2. 临时调高对应类别阈值。
3. 用户申诉时，admin → 用户管理 → 解封。

### Q: 自动封禁后如何撤销？
admin → 内容审核 → 日志详情 → "解封用户"按钮。等价于：
- 清空 `user_violations:<id>` Redis ZSET
- `user.status = enabled`

### Q: 哪些请求路径会被审核？
- 默认所有 `/v1/*`、`/v1beta/models/*:generateContent`、`/mj/*`、`/suno/*`、`/pg/*`。
- 跳过：`/v1/models`、`/v1/moderations`、`/v1/files`、`/v1/realtime`、`/health`。

## 监控指标

通过 `GET /api/admin/content_moderation/status` 获取（需 AdminAuth）：

- `worker.active_workers` / `queue_length` / `dropped` / `processed`
- `metrics.requests_total{layer,mode,action}` —— 复合 key
- `metrics.openai_latency_avg_ms` —— OpenAI 调用平均延迟
- `metrics.openai_errors_total{auth,rate_limit,timeout,other}`
- `metrics.auto_bans_total`
- `api_keys[].healthy` / `frozen_until` / `failure_count`
- `flagged_hash_count`

## 数据库 Schema

`content_moderation_logs` 表（三库通用，详见 `model/content_moderation_log.go`）：

- 主键 `id` int64
- 28 字段，包括请求归属（user / token / group / ip）、路由（endpoint / model / protocol）、决策（mode / action / detection_layer / flagged）、命中详情（highest_category / highest_score / category_scores JSON / threshold_snapshot JSON）、输入摘要（input_excerpt 已脱敏 / input_hash）、副作用（violation_count / auto_banned / email_sent）。
- 索引：单列 created_at / user_id / token_id / flagged / detection_layer / request_id / input_hash，复合 (user_id, created_at) / (flagged, created_at)。
- JSON 字段使用 TEXT，遵守 CLAUDE.md Rule 2。

## 架构最小侵入说明

本模块是 new-api fork 友好的设计：

- 上游修改累计 < 30 行后端：`model/main.go` +3、`main.go` +2、`router/relay-router.go` +1、`router/api-router.go` +1，合计 7 行。
- 上游修改累计 < 15 行前端：i18n locales 追加 key（不计入）。
- 完全零修改：`controller/relay.go`、`service/sensitive.go`、`setting/sensitive.go`、任何 `relay/channel/*`。
- 所有 CM 逻辑通过新增中间件 + 新增路由 + 新增 service/model 文件实现。

跟随上游更新只需 rebase；冲突面极小。
