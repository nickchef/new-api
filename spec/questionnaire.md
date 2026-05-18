# new-api 内容审核模块（Content Moderation）详细问卷

> 请逐题在 `> 答：` 后填写。**留空即视为采用斜体写出的"默认值/建议"**。
> 若觉得某个问题对你不重要，可直接写 `默认` 三个字。
> 完成后请告知，我将基于答案生成最终 spec。

1代表是（遵从你的建议），0代表否， 留空表示默认
---

## 阶段 2 方向小结（已确认）

- **核心策略**：完整移植 sub2api 设计，两层串联架构（L1 本地 AC 词库始终同步硬拦 + L2 OpenAI omni-moderation 按 mode 工作）
- **三种 mode**：`off` / `observe`（异步队列） / `pre_block`（同步拦截）
- **数据库**：SQLite/MySQL/PostgreSQL 三库通用，JSON 字段落 `TEXT`
- **Redis**：启用模块即必需
- **协议覆盖**：OpenAI / Claude / Gemini / 图像 / Embedding / MJ / Suno
- **多模态**：文本 + 图像（image_url / base64）
- **审核范围**：仅审核**输入**（用户 prompt）
- **配置粒度**：仅全局
- **流式**：与非流式同等处理
- **pre_block 超时**：默认 3000ms，超时 fail-open 放行 + 写 error 日志
- **错误格式**：随原请求协议适配（OpenAI/Claude/Gemini 各自格式）
- **API Key**：独立配置 + 多 Key 轮转 + 失败冻结
- **自动封禁**：默认 720h / 10 次
- **日志保留**：命中 180 天 / 未命中 3 天
- **采样率**：默认 100%
- **监控**：复用 new-api 现有指标体系
- **误判修正**：管理员手动删 hash + 手动解封
- **测试**：核心逻辑单测 + 关键路径集成测
- **前端**：只做 `web/default` 主题

---

## 第 1 章 触发与拦截逻辑

### 1.1 触发入口
**Q1.1.1** 替换/扩展 `controller/relay.go:126-143` 现有 `needSensitiveCheck` 钩子。是否同意把这里改成"先调本地 AC 词库 → 未命中再调 CM 模块"的两层结构？
*建议：同意，CM 模块作为独立 service 暴露 `CheckPrompt(ctx, input) Decision`，在词库放行后被调用。*
> 答： 

**Q1.1.2** 钩子是否仅作用于 `/v1/chat/completions`、`/v1/messages`、`/v1beta/models/*` 等 LLM 路径？还是连 `/v1/moderations` 自身也要审核？
*建议：`/v1/moderations` 跳过自审核（防止递归 + 用户本身在调审核接口）。*
> 答：1

**Q1.1.3** 对于 `/v1/audio/*`、`/v1/embeddings`、`/v1/rerank`、`/v1/images/*` 哪些路径**强制**走 CM？哪些路径**默认跳过**？
*建议：
- 强制：chat/completions、responses、messages、images/generations、images/edits、gemini models（含 generateContent/streamGenerateContent）
- 默认跳过：embeddings（无语义内容）、rerank、audio/speech（TTS 也是产生而非消费）、audio/transcriptions（STT 输入是音频，文本审核不适用）
- mj、suno：跟随主流程审核 prompt 字段*
> 答：1

**Q1.1.4** Playground (`/pg/chat/completions`) 是否也审核？
*建议：审核。playground 是用户行为入口，越狱测试常发于此。*
> 答：1

### 1.2 输入提取
**Q1.2.1** 文本提取的归一化策略？是否做小写转换、去标点、合并多余空白？
*建议：与 sub2api `content_moderation_input.go` 一致：
- 拼接所有 user/assistant/system 角色的 text 字段
- 去前后空白，但**不**做小写或去标点（OpenAI 模型自身归一化）
- 多段间用 `\n` 分隔
- 限制最大 32KB，截断时记录 `text_truncated=true`*
> 答：1

**Q1.2.2** 多模态消息中的 image_url：是直接传给 OpenAI Moderation API 的 image_url 参数，还是先下载本地再传 base64？
*建议：直接透传 image_url（OpenAI 端会自己抓取）。仅当是 base64 data: URL 时才解析后传。*
> 答：1

**Q1.2.3** 是否对图像数量做上限？
*建议：单次审核最多 8 张图（OpenAI omni-moderation 单次限制），超出截断并打 warn 日志。*
> 答：1

**Q1.2.4** Hash 计算输入：用归一化后的文本 + 图像 URL 列表，还是只用文本？
*建议：与 sub2api 一致：`sha256(normalized_text + sorted_image_urls)`，确保同样的多模态请求 hash 一致。*
> 答：1

**Q1.2.5** Claude `messages[].content` 中的 `tool_use` / `tool_result` 是否参与审核？
*建议：`tool_result` 参与（含外部数据风险），`tool_use` 内部参数跳过。*
> 答：1

**Q1.2.6** Gemini 的 `system_instruction` 和 `safetySettings` 中的文本是否参与审核？
*建议：`system_instruction` 参与（用户可注入），`safetySettings` 不参与。*
> 答：1

### 1.3 命中行为
**Q1.3.1** `pre_block` 模式命中时，是否立即关闭与上游的连接（如已建立）？
*建议：命中发生在调上游**之前**（审核在 PreConsumeBilling 之前），不会有上游连接。*
> 答：1

**Q1.3.2** 命中后退款（预扣费回退）逻辑：因为审核在预扣费之前，是否无需处理？
*建议：是。审核在 `service.PreConsumeBilling` 之前完成。但如果用户已通过 token 计费校验，需确保不写入正常日志，只写违规日志。*
> 答：1

**Q1.3.3** 在 `observe` 模式下，**本地词库**命中是否还要把入队任务发到 OpenAI 复查？
*建议：不要。本地词库命中已记日志，再发 OpenAI 浪费成本。但允许配置项 `observe_send_hits_to_openai`（默认 false）切换。*
> 答：1

---

## 第 2 章 OpenAI Moderation 调用

### 2.1 API Key 管理
**Q2.1.1** 配置项命名：`ContentModeration.APIKeys` 是数组还是用换行分隔的字符串？
*建议：换行分隔的字符串（与 new-api 现有 `SensitiveWords` 风格一致），存 option 表。运行时解析为 `[]string`。*
> 答：1

**Q2.1.2** API Key 轮转策略？
*建议：与 sub2api 一致：
- 默认 round-robin
- 单 Key 连续失败 N 次（默认 3）→ 冻结 1 分钟
- 所有 Key 都冻结 → fail-open 放行 + 写 error 日志*
> 答：1

**Q2.1.3** API Base URL 是否允许自定义（用于 Azure OpenAI Moderation 或代理）？
*建议：允许，默认 `https://api.openai.com`。和 channel 配置同样支持自定义 base_url。*
> 答：1

**Q2.1.4** 模型名是否允许自定义？
*建议：允许。默认 `omni-moderation-latest`，可改为 `text-moderation-latest` 或自部署模型。*
> 答：1

### 2.2 阈值与判定
**Q2.2.1** 13 个类别的默认阈值是否完全照搬 sub2api？
*建议：是。具体值：
- sexual/minors: 0.65（最敏感）
- self-harm/intent, self-harm/instructions, hate/threatening: 0.85
- violence/graphic, harassment/threatening: 0.90
- 其他类别：0.98*
> 答：1

**Q2.2.2** 阈值是否每类别可配？还是只配总开关？
*建议：每类别可配（Admin UI 给一个表格编辑，前端表单一键还原默认值）。*
> 答：1

**Q2.2.3** 命中判定逻辑：
*建议：只要有任一类别 `score >= threshold` 就判定 flagged。`highest_category` 取分数最高的类别，`highest_score` 是该分数。*
> 答：1

**Q2.2.4** 当 OpenAI 返回 `flagged=true` 但所有自定义阈值都未达到时，是否仍判定违规？
*建议：以自定义阈值为准，OpenAI 自身的 flagged 字段仅记录到日志的 `openai_flagged` 列。这给运营者最大控制权。*
> 答：1

### 2.3 超时与重试
**Q2.3.1** 单次调用超时？
*建议：默认 3000ms，可配 1000-30000ms。*
> 答：1

**Q2.3.2** 是否重试？
*建议：失败（5xx / 网络错误）重试 1 次，间隔 200ms。429 不重试，直接换 Key。*
> 答：1

**Q2.3.3** 总耗时（含重试 + 换 Key）超过 timeout × 1.5 后 fail-open？
*建议：是。最终上限 = `timeout * (retries + 1)`，超过即放弃。*
> 答：1

---

## 第 3 章 本地 AC 词库与 OpenAI 的协同

**Q3.1.1** 现有 `setting.SensitiveWords`、`setting.CheckSensitiveEnabled` 等开关是否保留 API 兼容？
*建议：保留。前端"敏感词管理"页面不动，仅新增"内容审核（CM）"独立配置页。*
> 答：1

**Q3.1.2** 本地词库命中后，日志写到现有 `model/log.go` 的统计日志，还是新的 `content_moderation_logs` 表？
*建议：写到新的 `content_moderation_logs` 表（统一查询），`detection_layer = local_keyword`。现有 SysLog 仅打 warn 行不动。*
> 答：1

**Q3.1.3** 本地词库命中是否计入"自动封禁阈值"的违规计数？
*建议：是。两层产生的违规都计入同一个计数器，但日志的 `detection_layer` 字段区分。*
> 答：1

---

## 第 4 章 数据模型

### 4.1 `content_moderation_logs` 表（新建）
**Q4.1.1** 表名最终确定？
*建议：`content_moderation_logs`（GORM 默认会复数化，需用 `TableName()` 锁定）。*
> 答：1

**Q4.1.2** 字段清单（请增删改）：
*建议：
| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGINT PK | GORM 自增 |
| created_at | BIGINT | unix 秒，与 new-api 风格一致 |
| user_id | INT | 关联 users.id（无外键约束，与 new-api 风格一致） |
| token_id | INT | 关联 tokens.id |
| channel_id | INT | 命中时使用的上游 channel（仅记录） |
| group_id | VARCHAR(64) | 用户组 |
| endpoint | VARCHAR(64) | 如 `/v1/chat/completions` |
| protocol | VARCHAR(32) | OpenAI / Claude / Gemini / Image / MJ / Suno |
| model | VARCHAR(128) | 用户请求的模型 |
| detection_layer | VARCHAR(16) | local_keyword / openai_moderation / hash_cache |
| mode | VARCHAR(16) | off / observe / pre_block |
| action | VARCHAR(16) | allow / block / hash_block / error |
| flagged | BOOLEAN | 是否触发违规 |
| blocked | BOOLEAN | 是否实际拦截（observe 命中时为 false） |
| highest_category | VARCHAR(64) | 命中最高分类别 |
| highest_score | FLOAT | 命中最高分数 |
| category_scores | TEXT | JSON 字符串，全部分类得分 |
| input_hash | VARCHAR(64) | sha256 hex |
| input_excerpt | VARCHAR(512) | 输入摘要（脱敏后） |
| image_count | INT | 图像数量 |
| local_words | TEXT | 命中的本地词（逗号分隔，仅 local_keyword 层填） |
| latency_ms | INT | 审核耗时 |
| queue_delay_ms | INT | observe 模式入队等待时间 |
| violation_count | INT | 窗口内累计违规（写入时计算） |
| auto_banned | BOOLEAN | 是否触发自动封禁 |
| email_sent | BOOLEAN | 是否已发邮件 |
| error_message | TEXT | 审核出错信息 |
| ip | VARCHAR(64) | 客户端 IP |
*
> 答：

**Q4.1.3** 索引设计？
*建议：
- 单列索引：created_at（按时间查询）、user_id、token_id、flagged、detection_layer
- 复合索引：(user_id, created_at) 用于"某用户最近违规"、(flagged, created_at) 用于"最近违规列表"
- input_hash 不建索引（hash 缓存在 Redis）*
> 答：

**Q4.1.4** `category_scores` 用 TEXT 存 JSON 字符串。是否需要在应用层提供 helper 自动 (un)marshal？
*建议：是。在 model 层用 GORM 钩子 `BeforeSave/AfterFind` 或 `database/sql.Scanner` 接口实现。优先用 `common.Marshal/Unmarshal`（Rule 1）。*
> 答：

### 4.2 配置项（option 表）
**Q4.2.1** 配置项命名前缀？
*建议：`ContentModeration` 前缀，与 new-api 现有 `Sensitive*` 风格一致。如：
- `ContentModerationEnabled` (bool)
- `ContentModerationMode` (string: off/observe/pre_block)
- `ContentModerationAPIKeys` (text, 换行分隔)
- `ContentModerationBaseURL` (string)
- `ContentModerationModel` (string)
- `ContentModerationTimeoutMS` (int)
- `ContentModerationRetryCount` (int)
- `ContentModerationSampleRate` (int, 0-100)
- `ContentModerationThresholds` (text, JSON)
- `ContentModerationWorkerCount` (int)
- `ContentModerationQueueSize` (int)
- `ContentModerationPreHashCheckEnabled` (bool)
- `ContentModerationRecordNonHits` (bool)
- `ContentModerationBlockStatus` (int, 默认 403)
- `ContentModerationBlockMessage` (text, i18n key 或字面量)
- `ContentModerationHitRetentionDays` (int)
- `ContentModerationNonHitRetentionDays` (int)
- `ContentModerationAutoBanEnabled` (bool)
- `ContentModerationBanThreshold` (int)
- `ContentModerationViolationWindowHours` (int)
- `ContentModerationEmailOnHit` (bool)
- `ContentModerationEmailToAdmin` (bool)
- `ContentModerationEmailToUser` (bool)
- `ContentModerationObserveSendHitsToOpenAI` (bool, 默认 false)*
> 答：

**Q4.2.2** 配置如何热生效？
*建议：与 new-api 现有 option 表的 `LoadOption()` + 在内存里维护一份 `ContentModerationConfig` 结构（带 sync.RWMutex），option 表更新时通过 hook 触发 reload。Worker 在循环顶部读最新 config（与 sub2api 一致）。*
> 答：

### 4.3 Redis 数据结构
**Q4.3.1** Hash 黑名单 Key 命名？
*建议：`new_api:content_moderation:flagged_hashes`（与 new-api 现有 Redis Key 前缀风格一致）。*
> 答：

**Q4.3.2** Hash 数据结构：Set 还是 Hash？
*建议：Set（与 sub2api 一致），`SADD/SISMEMBER` O(1)。*
> 答：

**Q4.3.3** Hash 是否设 TTL？
*建议：不设。永久保留直到管理员手动删除或清空。但提供 `purge_older_than` 后台脚本，按时间维度清理（需把 hash → timestamp 用单独的 Hash 类型存储，添加复杂度，**默认不实现**，仅记入未来增强）。*
> 答：

**Q4.3.4** 违规计数器 Key 命名？
*建议：`new_api:content_moderation:user_violations:<user_id>`，类型 ZSET（score = timestamp），用 `ZADD` + `ZREMRANGEBYSCORE` 维护滑动窗口。`ZCARD` 拿当前计数。*
> 答：

---

## 第 5 章 异步队列与 Worker

**Q5.1.1** Worker 数量默认值与上限？
*建议：默认 4，上限 32（与 sub2api 一致）。*
> 答：

**Q5.1.2** 队列容量默认值与上限？
*建议：默认 32768，上限 100000。*
> 答：

**Q5.1.3** Worker goroutine 启动时机？
*建议：在 main.go 的 `InitResources` 调用一个 `service.InitContentModeration()`，根据配置启动 worker。CM 模块禁用时不启动。*
> 答：

**Q5.1.4** Worker 在 `mode=off` 或 `enabled=false` 时是否仍保持空转？
*建议：保持空转（每秒 reload 一次配置，秒级响应配置切换）。与 sub2api 一致。*
> 答：

**Q5.1.5** 队列满时丢弃 vs 阻塞？
*建议：非阻塞写（`select default` 丢弃 + Warn 日志 + Drop 计数器 +1），绝不阻塞业务请求。*
> 答：

**Q5.1.6** 进程关闭时是否优雅排空队列？
*建议：在 main.go 监听 SIGTERM，给 worker 5 秒处理剩余任务，超时强制退出。*
> 答：

---

## 第 6 章 协议适配

### 6.1 OpenAI 系
**Q6.1.1** `/v1/chat/completions` 输入提取：拼接所有 `messages[].content` 中的 text + image 部分。是否包含 system 与 assistant 角色？
*建议：包含。某些越狱 prompt 会塞到 system role 里。assistant role 含义复杂（few-shot 示例），但攻击者可放越狱内容，建议**也包含**。*
> 答：

**Q6.1.2** `/v1/completions`（旧版） `prompt` 字段：可能是 string、string[]、token list（[]int）。审核策略？
*建议：string 与 string[] 拼接审核；token list 反向解码代价高，跳过审核+打 warn 日志。*
> 答：

**Q6.1.3** `/v1/responses` 的 `input` 字段（OpenAI 新版 Responses API）：
*建议：与 chat 接口同等处理，递归遍历 input 提取 text/image。*
> 答：

### 6.2 Claude
**Q6.2.1** `/v1/messages` 的 `system` 字段（可以是 string 或 array）参与审核：
*建议：参与。*
> 答：

**Q6.2.2** `messages[].content` 中的 `tool_result.content`（工具返回结果）：参与还是跳过？
*建议：参与。攻击者可通过工具返回注入。但限制提取深度（防嵌套 DoS）。*
> 答：

### 6.3 Gemini
**Q6.3.1** Gemini 的 `contents[].parts[]` 包含 text / inline_data（base64 图像）/ file_data（GCS URI）。审核覆盖哪些？
*建议：text + inline_data（图像）。file_data 跳过（GCS URI 一般是平台资源，且抓取需 auth）。*
> 答：

**Q6.3.2** `systemInstruction` 参与吗？
*建议：参与。*
> 答：

### 6.4 图像 / MJ / Suno
**Q6.4.1** `/v1/images/generations` 与 `/v1/images/edits` 的 `prompt` 字段审核：
*建议：审核。文生图风险点。edits 接口的图像本身也参与（与 image_url 流程一致）。*
> 答：

**Q6.4.2** Midjourney `/mj/submit/imagine` 的 prompt 提取：
*建议：审核 prompt 字段（"--ar 16:9" 等参数不审核）。代码需识别 MJ 私有 DSL。*
> 答：

**Q6.4.3** Suno 的歌词字段是否审核？
*建议：审核。*
> 答：

### 6.5 跳过策略
**Q6.5.1** 哪些路径**硬编码不审核**（无论配置如何）？
*建议：
- `/v1/moderations` 自身
- `/v1/models`、`/v1/models/:model`（只读元数据）
- `/v1/files`（未实现）
- 健康检查、登录、注册等非 relay 路径*
> 答：

---

## 第 7 章 错误响应格式

**Q7.1.1** OpenAI 路径拦截响应：
*建议：
```json
{
  "error": {
    "message": "<BlockMessage>",
    "type": "content_policy_violation",
    "param": null,
    "code": "content_moderation_blocked"
  }
}
```
HTTP 403。复用 `service.OpenAIErrorWrapper` 风格。*
> 答：

**Q7.1.2** Claude 路径拦截响应：
*建议：
```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "<BlockMessage>"
  }
}
```
HTTP 400（与 Anthropic 真实策略一致），或改用 403。*
> 答：

**Q7.1.3** Gemini 路径拦截响应：
*建议：
```json
{
  "error": {
    "code": 400,
    "message": "<BlockMessage>",
    "status": "INVALID_ARGUMENT"
  }
}
```
*
> 答：

**Q7.1.4** BlockMessage 是否支持 i18n？
*建议：是。i18n key `MsgContentModerationBlocked`，参数化 `{Category}`、`{Hash}`。中文/英文模板由配置覆盖。*
> 答：

---

## 第 8 章 自动封禁与告警

### 8.1 自动封禁
**Q8.1.1** 封禁判定时机？
*建议：每次写入 flagged=true 日志后立即查 ZSET 计数，超过阈值即更新 user.status='banned'。同步事务。*
> 答：

**Q8.1.2** 封禁与现有 user.status 的关系？
*建议：复用现有 `common.UserStatusDisabled`（不新加 status 值），但日志中标记 `auto_banned=true` 以与人工封禁区分。*
> 答：

**Q8.1.3** 解封是否需要二次审批？
*建议：不需要，admin 一次操作生效。但记入 SysLog。*
> 答：

**Q8.1.4** 封禁后用户已有 token 的处理？
*建议：复用 new-api 现有逻辑（status disabled 后 TokenAuth 中间件会拒绝）。无需特殊处理。*
> 答：

### 8.2 邮件告警
**Q8.2.1** 给用户的邮件内容？
*建议：
- 主题："您的账号因违反内容审核策略已被限制使用"
- 正文：违规类别、违规时间、命中数、申诉方式（admin 联系邮箱）
- 不透露具体词或得分*
> 答：

**Q8.2.2** 给管理员的邮件？
*建议：
- 主题：内容审核告警 - 用户 #<id> 触发违规
- 正文：用户邮箱、违规类别、命中数、输入摘要（脱敏）、跳转管理后台链接
- 仅"自动封禁"事件触发，单纯命中不发（避免邮件爆炸）*
> 答：

**Q8.2.3** 邮件发送是否限频？
*建议：是。同一用户 24h 内最多发 1 封通知邮件。Redis Key `new_api:content_moderation:email_sent:<user_id>` TTL 86400。*
> 答：

---

## 第 9 章 Admin API

**Q9.1.1** Endpoints 清单：
*建议：
- `GET /api/admin/content-moderation/config` 查配置
- `PUT /api/admin/content-moderation/config` 更新配置
- `GET /api/admin/content-moderation/status` 运行状态（队列、worker、计数器）
- `POST /api/admin/content-moderation/test-api-keys` 测试 Key 连通性
- `POST /api/admin/content-moderation/preview` 预审某段文本/图像
- `GET /api/admin/content-moderation/logs` 日志列表（支持 user_id、flagged、time_range 过滤）
- `GET /api/admin/content-moderation/logs/:id` 日志详情
- `DELETE /api/admin/content-moderation/flagged-hash` body: {hash} 删除黑名单
- `GET /api/admin/content-moderation/flagged-hash/count` 黑名单大小
- `POST /api/admin/content-moderation/flagged-hash/clear` 清空黑名单
- `POST /api/admin/content-moderation/unban/:user_id` 解封用户*
> 答：

**Q9.1.2** Auth：所有 endpoints 都用 `middleware.AdminAuth()`？
*建议：是。*
> 答：

**Q9.1.3** 是否需要 `root` 权限和普通 admin 权限区分？
*建议：不需要。所有 CM admin 操作复用 `middleware.AdminAuth()`，无需 root。*
> 答：

---

## 第 10 章 前端管理 UI

### 10.1 页面规划
**Q10.1.1** 在 `web/default/src/` 新增页面，注册到哪个菜单？
*建议：在"设置"模块下新增子菜单"内容审核"。或在"操作设置"页内新增 tab。*
> 答：

**Q10.1.2** 页面数量：
*建议：
1. 配置页（含全部 option 表单 + API Key 测试按钮）
2. 日志列表页（支持过滤、分页）
3. 黑名单管理页（hash 列表、删除、清空）
4. 监控状态页（队列水位、worker 状态、近 24h 命中统计）
*
> 答：

**Q10.1.3** 阈值配置 UI：13 个 slider 还是 13 个数字输入？
*建议：13 行的表格，每行：类别名 + 数字输入框（0-1，步长 0.01）+ "恢复默认"按钮。表格顶部一个"全部恢复默认"。*
> 答：

### 10.2 i18n
**Q10.2.1** 翻译语言覆盖？
*建议：中文 + 英文。其他语言（fr/ru/ja/vi）由社区翻译，源文件留英文 fallback。*
> 答：

---

## 第 11 章 非功能需求

### 11.1 性能
**Q11.1.1** pre_block 模式下端到端额外延迟目标：
*建议：P50 < 300ms，P99 < 1500ms（在 OpenAI 正常响应时）。*
> 答：

**Q11.1.2** observe 模式下主链路额外延迟：
*建议：P99 < 10ms（只做 hash 提取 + 入队 + 本地词库检查）。*
> 答：

**Q11.1.3** 单实例支持的 QPS 上限？
*建议：与 new-api 主流程相当（即审核模块不应成为瓶颈）。worker 数量自适应即可。*
> 答：

### 11.2 可用性
**Q11.2.1** OpenAI Moderation API 不可达时的降级策略？
*建议：fail-open（放行）+ 写 error 日志 + Worker 错误计数器递增 + Prometheus 报警。本地词库不受影响。*
> 答：

**Q11.2.2** Redis 不可达时？
*建议：CM 模块整体降级为本地词库 only（Hash 缓存、违规计数都失效）。打 fatal 日志告警。*
> 答：

**Q11.2.3** 数据库写日志失败时？
*建议：写失败仅打 error 日志，不影响主链路。日志可能丢失，运营接受。*
> 答：

### 11.3 安全
**Q11.3.1** OpenAI API Key 在配置存储时是否加密？
*建议：与现有 channel.key 一致：option 表明文存储，依赖数据库访问控制。**但** API 返回前端时脱敏（只显示前 4 + 后 4）。*
> 答：

**Q11.3.2** 日志的 `input_excerpt` 字段是否需要脱敏（防止违规内容被运维看到二次伤害）？
*建议：是。固定提取前 200 字符 + 末尾省略号；含 PII 模式（邮箱/手机号/身份证）的替换为占位符。*
> 答：

**Q11.3.3** Admin API 是否需要 rate limit？
*建议：复用现有 admin auth + IP rate limit（如有）即可。*
> 答：

---

## 第 12 章 监控与可观测性

**Q12.1.1** 复用 `pkg/perf_metrics` 暴露哪些指标？
*建议：
- `content_moderation_requests_total{layer,mode,action}`
- `content_moderation_latency_ms{layer}` histogram
- `content_moderation_queue_length` gauge
- `content_moderation_queue_dropped_total` counter
- `content_moderation_workers_active` gauge
- `content_moderation_openai_errors_total{type}` counter
- `content_moderation_auto_bans_total` counter*
> 答：

**Q12.1.2** 结构化日志关键字段？
*建议：与 sub2api 一致：user_id, api_key_id, endpoint, protocol, mode, flagged, blocked, action, highest_category, highest_score, latency_ms, queue_delay_ms, input_hash, error*
> 答：

**Q12.1.3** 日志输出库？
*建议：复用 new-api 现有 `common.SysLog`、`logger.LogWarn` 等，不引入新依赖。*
> 答：

---

## 第 13 章 数据库迁移

**Q13.1.1** 迁移脚本风格？
*建议：参考 new-api 现有方式（GORM AutoMigrate + `model/main.go` 的迁移片段），不引入独立的 migrations 目录。*
> 答：

**Q13.1.2** 迁移幂等性？
*建议：GORM AutoMigrate 天然幂等。手写 `ALTER TABLE` 时需用 `IF NOT EXISTS` 兼容三库（SQLite 不支持，需先查询信息表）。*
> 答：

**Q13.1.3** 升级失败回滚策略？
*建议：不提供向下迁移。CM 模块表为新增，删除即可。*
> 答：

---

## 第 14 章 测试

### 14.1 单测
**Q14.1.1** 必须覆盖的核心函数清单：
*建议：
- 输入提取（每个协议一个 testdata fixture）
- 阈值判定
- API Key 轮转 / 冻结
- Hash 计算的确定性（同输入 hash 一致）
- 异步队列入队/丢弃逻辑
- 违规计数器滑窗
- fail-open / fail-close 切换*
> 答：

**Q14.1.2** Mock OpenAI Moderation API 的方式？
*建议：用 `httptest.NewServer` 启动假端，service 配置指向它。覆盖正常、429、500、超时、格式异常 5 个场景。*
> 答：

### 14.2 集成测
**Q14.2.1** 集成测覆盖路径：
*建议：
- `/v1/chat/completions` + observe 模式 → 200 通过 + 日志写入
- `/v1/chat/completions` + pre_block 模式 + 命中 → 403
- `/v1/messages` + 命中 → 400 with Claude 格式
- `/v1beta/models/*:generateContent` + 命中 → 400 with Gemini 格式
- 本地词库命中 → 不调 OpenAI
- Hash 缓存命中 → 不调 OpenAI
- 自动封禁触发*
> 答：

**Q14.2.2** 集成测用哪个数据库？
*建议：SQLite（速度快，单测友好）。MySQL/PG 仅手动验证 schema 创建。*
> 答：

### 14.3 验收
**Q14.3.1** 必须通过的端到端场景：
*建议：
- S1：上线后默认 enabled=false，主流程零影响
- S2：开启 observe 模式 + 跑 100 条正常请求 → 0 误拦截 + 日志全量记录
- S3：跑 sub2api 现成的越狱 prompt 集 → 命中率与 sub2api 在同数据集上对齐（差异 ≤5%）
- S4：切 pre_block + 跑相同集合 → 命中拦截
- S5：连续触发 10 次 → 用户自动封禁 + 邮件送达
- S6：删除黑名单 hash → 后续相同输入重新走 OpenAI*
> 答：

---

## 第 15 章 部署与上线

**Q15.1.1** 灰度策略？
*建议：
1. 默认 enabled=false 合入主分支
2. 测试环境开启 observe 一周
3. 调阈值后开启 pre_hash_check_enabled 让缓存预热
4. 生产环境先开 observe，跑 1-2 周
5. 切 pre_block 上线*
> 答：

**Q15.1.2** 回滚条件？
*建议：
- 误拦截率（人工 review 后判误）> 1%
- 主链路 P99 延迟增长 > 200ms（observe 模式下）
- OpenAI 调用月成本超出预算（具体数字待定）
任一触发即降级回 observe 或关闭 CM。*
> 答：

**Q15.1.3** 文档更新清单？
*建议：
- README 增加内容审核章节
- AGENTS.md / CLAUDE.md 不更新（开发约定无变化）
- docs/ 目录新增 content_moderation.md*
> 答：

---

## 第 16 章 风险与开放问题

**Q16.1.1** 你最担心哪个环节？（自由作答）
> 答：

**Q16.1.2** 是否有 sub2api 那边踩过的坑你已经知道、希望本次直接规避的？
> 答：

**Q16.1.3** 是否有未列出的功能需求？
> 答：

**Q16.1.4** 是否有 deadline？
> 答：

---

**填写完毕后，请回复"问卷已填完"或"开始生成 spec"，我将基于答案产出 spec/overview.md、spec/goals.md、spec/context.md，并在 AGENTS.md 追加规格创建记录。**
