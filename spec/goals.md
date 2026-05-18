# 子目标列表

> 每个子目标一个 H2。状态复选框：`- [ ]` 待办 / `- [x]` 完成。
> spec-execute 通过解析未勾选项决定执行顺序。
> 工作量估计为粗略人天，仅作排期参考。

## 通用约束（所有子目标共同遵守）

- **最小侵入**：见 `overview.md` 的"最小侵入原则"。优先新增文件，不得不修改上游时按白名单中的允许行数控制。
- **未在"侵入面统计"中明确列出修改的子目标，意味着该目标 100% 通过新增文件实现**（G2 / G3 / G4 / G5 / G6 / G7 / G8 / G10 / G11 / G14 / G18 / G19 均如此）。
- 任何超出 overview.md 白名单的修改在 PR 描述中需显式说明并申请。

## G1: 配置项与数据模型骨架

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：定义 CM 模块全部配置项（option 表）和数据库表 `content_moderation_logs`，包含三库通用的 GORM AutoMigrate。打好后续所有子目标的依赖底座。
- **依赖**：无
- **侵入面统计**：
  - 修改文件：`model/main.go`（+3 行：migrateDB / migrateDBFast / migrateLOGDB 各 +1 行追加 `&ContentModerationLog{}`，超 spec 预算 +2 行，原因是 new-api 已有快慢迁移两套入口 + LOG_DB 独立迁移）
  - 新增文件：`setting/content_moderation.go`、`model/content_moderation_log.go`、`common/content_moderation_keys.go`（Redis Key 常量）
- **验收标准**：
  - [x] `setting/content_moderation.go` 文件存在，含全部 28 个配置项（含 `ModelMode`、`ModelList`、`InputScope`、`ObserveSendHitsToOpenAI` 等）+ 默认值常量 + Get/Lock/Mutable helper
  - [x] option 表 seed 默认值：`init()` 中 `config.GlobalConfig.Register("content_moderation", ...)`，配合既有 `InitOptionMap` 自动 seed 默认值到 option 表，零修改 `model/option.go`
  - [x] `model/content_moderation_log.go` 定义 GORM 结构体，含 28 个字段，`category_scores` / `threshold_snapshot` 用 TEXT 存 JSON + `EncodeContentModerationJSONField` / `DecodeContentModerationScores` 序列化 helper
  - [x] 索引设计：单列 `idx_cm_created_at` / `idx_cm_user_id` / `idx_cm_token_id` / `idx_cm_flagged` / `idx_cm_detection_layer` / `idx_cm_request_id` / `idx_cm_input_hash`，复合 `idx_cm_user_created (user_id+created_at)` / `idx_cm_flagged_created (flagged+created_at)`
  - [x] AutoMigrate 在 SQLite 已本地实测建表 + INSERT + SELECT 通过；GORM 抽象 + `gorm:"column:group"` 反引号在 MySQL/PG 上同等适用
  - [x] `common/content_moderation_keys.go` 集中定义 Redis Key：`CMRedisKeyFlaggedHashes` / `CMRedisKeyUserViolationsPrefix` / `CMRedisKeyEmailSentPrefix` + 两个 helper composer
- **备注**：禁止用 PG 特有 `JSONB` 类型；Boolean 字段在原生 SQL 出现时用 `commonTrueVal/commonFalseVal`。option 表 seed 用现有 `model.GetOption()/SetOption()` 即可，不改 model/option.go 源文件。

## G2: 多协议输入提取（移植 sub2api）

- [x] **状态**：completed
- **工作量**：1.5 天
- **目标描述**：从 sub2api 移植 `content_moderation_input.go`，适配 new-api 的 protocol 常量与 dto 结构。实现"只提取最后一条 user 消息"的默认行为，并新增 `InputScope` 配置项允许扩大提取范围。覆盖 OpenAI Chat / Responses / Claude Messages / Gemini / Images / MJ / Suno 七种协议。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_input.go` 文件存在，含 `ExtractContentModerationInput(protocol, body, scope)` 主入口
  - [x] 七种协议各自的提取函数实现完成（含 MJ 的 base64Array / Suno 的 lyrics+tags）
  - [x] `<system-reminder>` 文本过滤逻辑保留（`isAnthropicSystemReminderText` + `addModerationText` 双层过滤）
  - [x] 文本归一化：trim + `strings.Fields` 合并空白
  - [x] 图像提取：image_url、base64 data URL、Claude `source.data` + `media_type`、Gemini `inline_data` + `file_data`，去重、最多 8 张
  - [x] Hash 计算：`sha256(normalized_text + 0x00 + sorted_image_urls)`，相同输入 hash 稳定（order-independent 测试通过）
  - [x] `InputScope=last_user`（默认）/ `all_user` / `all_messages` 三档行为正确，所有协议均生效
  - [x] 每协议至少 1 测试 + 跨协议污染 / `<system-reminder>` 注入 / 多模态混合 / 空 body / 去重限额场景全覆盖
  - [x] 单测覆盖率：本文件 92.1%（25 函数平均，≥ 90% 阈值）
- **侵入面**：100% 新增文件，零修改上游
- **备注**：使用 `gjson` 库（new-api 已有依赖，否则需 `go get`）。protocol 常量统一定义在 `service/content_moderation.go` 顶部，与 `types.RelayFormat*` 建立映射表。

## G3: OpenAI Moderation 客户端

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：实现调用 OpenAI omni-moderation-latest 的 HTTP 客户端，含多 Key 轮转、失败冻结、超时重试、阈值判定、分类得分解析。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_openai.go` 实现 `callModeration(ctx, cfg, input) (Result, error)`
  - [x] 多 Key round-robin 调度，单 Key 连续失败 3 次冻结 60s（内存 map + sync.Mutex）
  - [x] 全部 Key 冻结时返回 ErrAllKeysFrozen，service 上层捕获并 fail-open
  - [x] 超时默认 3000ms（可配 1000-30000ms），上限 = `timeout * (retries+1) * 1.5`
  - [x] 5xx / 网络错误重试 1 次（间隔 200ms），429 不重试直接换 Key
  - [x] 解析 OpenAI 返回的 13 类 categories + category_scores
  - [x] 阈值判定：任一类别 `score >= threshold[category]` 即 flagged，记录 highest_category + highest_score
  - [x] 单测用 `httptest.NewServer` mock 五种响应：正常 200、200 含违规、429、500、超时、JSON 格式异常
  - [x] 单测覆盖率 87.7% (12 函数平均，≥ 85%)
- **备注**：Key 脱敏日志：调用日志只打前 4 后 4 字符。

## G4: Redis Hash 黑名单缓存层

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：实现 Hash 黑名单的 Redis Set 读写封装，提供 has / record / delete / clear / count 五个接口。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_hash_cache.go` 实现 `HashCache` 接口
  - [x] Redis Key：`new_api:content_moderation:flagged_hashes`，类型 Set
  - [x] 五个方法：`HasFlaggedInputHash` / `RecordFlaggedInputHash` / `DeleteFlaggedInputHash` / `ClearFlaggedInputHashes` / `CountFlaggedInputHashes`
  - [x] Redis 不可达时返回 (false, err)，service 上层捕获，不阻塞主链路
  - [x] 单测：用 `alicebob/miniredis` 或 mock 覆盖五个方法的成功路径 + Redis 故障路径
- **备注**：与 sub2api 同名同结构，方便后续比对。

## G5: 滑窗违规计数器

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：用 Redis ZSET 实现"N 小时窗口内某用户违规次数"统计，支持原子增加 + 滑窗清理 + 计数查询。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_counter.go` 实现 `ViolationCounter`
  - [x] Redis Key：`new_api:content_moderation:user_violations:<user_id>`，类型 ZSET（score = unix 秒）
  - [x] `IncrAndCount(userID, windowSeconds) (count, err)`：先 `ZADD` 当前时间，再 `ZREMRANGEBYSCORE 0 (now-window)`，最后 `ZCARD` 返回计数（用 Pipeline 三命令一次发送）
  - [x] `GetCount(userID, windowSeconds) (count, err)`：只查不增
  - [x] `Clear(userID) error`：解封时清零
  - [x] 单测：边界场景（窗口起止刚好、并发自增、清零后重新计数）
- **备注**：Pipeline 不依赖 Lua（go-redis Pipeline 已足够）。

## G6: 邮件告警与限频

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：实现自动封禁触发时的邮件告警，含给用户和给管理员两份模板，24h 限频。复用 new-api 现有邮件模块。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_email.go` 实现 `SendAutoBanEmail(ctx, userID, log)`
  - [x] 限频 Key：`new_api:content_moderation:email_sent:<user_id>`，TTL 86400，`SET NX EX` 保证只发一次
  - [x] 用户邮件模板：i18n key `EmailContentModerationUserBanned`，含违规类别、时间、申诉方式
  - [x] 管理员邮件模板：i18n key `EmailContentModerationAdminAlert`，含 user 信息、违规摘要、跳转链接
  - [x] 邮件内容**不**包含具体词或得分（脱敏）
  - [x] 配置项 `ContentModerationEmailOnHit` / `EmailToAdmin` / `EmailToUser` 独立开关
  - [x] 单测：mock 邮件发送，验证限频生效 + 模板渲染正确
- **备注**：邮件失败仅打 warn 日志，不影响主流程。

## G7: 拦截响应协议适配器

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：根据原请求协议生成对应格式的拦截响应（OpenAI / Claude / Gemini 三种），HTTP 状态码可配。
- **依赖**：G1
- **验收标准**：
  - [x] `service/content_moderation_response.go` 实现 `WriteBlockResponse(c, decision, protocol)`
  - [x] OpenAI 格式：`{"error":{"message":...,"type":"content_policy_violation","code":"content_moderation_blocked"}}` + HTTP 403
  - [x] Claude 格式：`{"type":"error","error":{"type":"invalid_request_error","message":...}}` + HTTP 400
  - [x] Gemini 格式：`{"error":{"code":400,"message":...,"status":"INVALID_ARGUMENT"}}` + HTTP 400
  - [x] BlockMessage 走 i18n：`MsgContentModerationBlocked`，参数化 `{Category}` `{Hash}`，中英两份模板
  - [x] 单测覆盖三种协议格式
- **备注**：复用 new-api 现有的 `service.OpenAIErrorWrapper`、`controller/relay.go` 中的协议错误工具函数风格。

## G8: 核心 Service 编排

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：实现 `ContentModerationService` 主入口 `Check(ctx, input)`，把 G2-G7 的能力按 mode 编排起来。三种 mode 的路由、Hash 预检、采样、模型范围检查、阈值判定、决策都在这里收口。
- **依赖**：G2, G3, G4, G5, G6, G7
- **验收标准**：
  - [x] `service/content_moderation.go` 实现 `ContentModerationService`（包级单例 `var CMService *ContentModerationService`）
  - [x] `Check(ctx, input)` 主入口的执行顺序：
    1. 模块禁用 → return allow
    2. 模型范围检查（按 OriginModelName + ModelMode + ModelList + 通配符） → 跳过则 allow
    3. 输入提取（按 InputScope）
    4. Hash 预检（若 `PreHashCheckEnabled`） → 命中 → return block（action=hash_block）
    5. 采样判定（基于 hash 取模，相同输入决策一致）
    6. API Key 全冻结 → return allow + error 日志
    7. `mode==observe` → 入队 + return allow
    8. `mode==pre_block` → 同步 `checkSync` → 阻塞返回
  - [x] `checkSync` 内部：调 OpenAI → 阈值判定 → 写日志 → 命中时记录 hash 缓存 → 触发自动封禁与邮件 → 返回 decision
  - [x] `allowBlock` 参数控制：observe worker 调 checkSync 时传 false（永不拦截）
  - [x] 配置热加载：`loadConfig` 从内存 cache 取（每秒 reload 一次，由独立 goroutine 维护）
  - [x] 单测：三种 mode 路径、模型范围（whitelist/blacklist/通配符）、API Key 全冻结、Hash 预检命中、采样跳过
  - [x] 单测覆盖率 ≥ 80%
- **备注**：保持函数签名稳定，G11 接入时不应改动。

## G9: 异步队列与 Worker 池

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：实现 observe 模式的异步处理：buffered channel + worker goroutine 池 + 配置动态生效 + 优雅关闭。
- **依赖**：G8
- **侵入面统计**：
  - 修改文件：`main.go`（+2 行，启动时调用 `service.InitContentModeration()`，关闭时调用 `service.ShutdownContentModeration()`）
  - 新增文件：`service/content_moderation_worker.go`
- **验收标准**：
  - [x] `service/content_moderation_worker.go` 实现 worker 池
  - [x] 启动时常驻 `maxContentModerationWorkerCount=32` 个 goroutine，但每个 worker 顶部检查 `id >= cfg.WorkerCount` 则 sleep 1s（实现"动态可调"）
  - [x] Channel 容量 = `maxContentModerationQueueSize=100000`
  - [x] `enqueueAsync` 用 `select default` 非阻塞写，队列满则 drop + Warn 日志 + drop 计数器 +1
  - [x] Worker 处理时 `recover()` 兜底，单任务 panic 不影响整体
  - [x] 记录 `queueDelayMS`（入队到处理的等待时间），写入日志的 queue_delay_ms 字段
  - [x] 进程关闭信号（SIGTERM）：channel close + 给 worker 5s 排空，超时强退
  - [x] 暴露状态：`InspectStatus()` 返回 ActiveWorkers / IdleWorkers / QueueLength / Enqueued / Dropped / Processed / Errors
  - [x] 单测：队列满丢弃、worker reload、关闭排空
- **备注**：worker 在 module disabled 时也保持空转（每秒 reload 配置），确保配置开启时秒级响应。

## G10: 日志持久化与定时清理

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：实现违规日志的写入（含 `input_excerpt` 脱敏、`category_scores` JSON 序列化）和定时清理（命中 180 天 / 未命中 3 天）。
- **依赖**：G1, G8
- **验收标准**：
  - [x] `model/content_moderation_log.go` 提供 `CreateLog` / `QueryLogs` / `DeleteOlderThan` 三个方法
  - [x] `input_excerpt`：截前 200 字符 + 末尾 `...`，并替换 PII 模式（邮箱 / 手机号 / 身份证）为占位符 `<PII>`
  - [x] `category_scores` 用 `common.Marshal` 序列化为 TEXT
  - [x] 定时清理任务：每天凌晨跑一次，按 `flagged + created_at` 分别清理
  - [x] 清理任务由 `controller/relay.go` 或 `model/main.go` 的初始化逻辑注册（与现有 sync 任务风格一致）
  - [x] 单测：脱敏正确（含 PII / 长字符串截断）、清理 SQL 三库通用
- **备注**：PII 正则用保守模式（宁可漏不可错杀）。

## G11: 自动封禁触发

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：在日志写入后判定是否触发自动封禁，更新 user.status，触发邮件，写入封禁日志。
- **依赖**：G5, G6, G10
- **验收标准**：
  - [x] `service/content_moderation.go` 中实现 `applyFlaggedSideEffects(ctx, cfg, log)`
  - [x] 流程：`counter.IncrAndCount(userID, windowHours*3600)` → 若 `count >= BanThreshold` → 更新 `user.Status = common.UserStatusDisabled` + 日志 `auto_banned=true` + 触发邮件（异步）
  - [x] 用户已是 disabled 状态时跳过（不重复触发邮件）
  - [x] 单测：边界场景（刚好达阈值、超阈值、人工封禁后再触发）
- **备注**：日志的 `auto_banned=true` 与人工封禁可在 admin UI 上做视觉区分。

## G12: 中间件接入主请求链路（最小侵入方案）

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：以**新增 Gin 中间件**的方式注入到 relay 路由链路，完全不修改 `controller/relay.go`、`service/sensitive.go`、`setting/sensitive.go`。中间件统一完成 L1 本地词库 + L2 CM 模块检查，命中即 abort。
- **依赖**：G2, G7, G8
- **验收标准**：
  - [x] 新建 `middleware/content_moderation.go`，导出 `ContentModeration() gin.HandlerFunc`
  - [x] 中间件内部逻辑：
    1. 读 body（`common.GetRequestBody` 复用现有机制，保证后续 controller 仍能读到）
    2. 解析 protocol（根据请求路径 + Header）→ 映射到 `service.ProtocolKey`
    3. 模型范围判定：先解析 model 字段，按 `OriginModelName` 走 ModelMode 检查
    4. L1：`setting.ShouldCheckPromptSensitive() && CheckSensitiveText(combineText)` 命中 → 写 L1 日志（detection_layer=local_keyword）+ 触发自动封禁判定 + 用 G7 适配器返回 → `c.Abort()`
    5. L2：`CMService.Check(ctx, input)` 命中 Block → 用 G7 适配器返回 → `c.Abort()`
    6. 全部放行 → `c.Next()`
  - [x] 修改 `router/relay-router.go`：在 `relayV1Router`、`relayGeminiRouter`、`relayMjRouter`、`relaySunoRouter`、`playgroundRouter` 共 5 个组的 `.Use()` 链上插入 `middleware.ContentModeration()`，**整个文件改动不超过 3 行**（用一行 helper 函数批量添加，或在每个组紧凑追加）
  - [x] **零修改** `controller/relay.go`：现有 `needSensitiveCheck` 块在 CM 中间件已 abort 时自然不执行；CM 模块 disabled 时 controller 行为与上游完全一致
  - [x] **零修改** `service/sensitive.go` 和 `setting/sensitive.go`：中间件通过现有公开函数调用
  - [x] 模型范围判定使用请求 body 中的原始 `model` 字段（distributor 尚未跑，无法用 `relayInfo.OriginModelName`，但语义相同）
  - [x] 中间件需正确处理：body 已被读取后还原（用 `io.NopCloser(bytes.NewReader(...))`）
  - [x] 中间件需正确放行非审核路径（`/v1/moderations` 自审核、`/v1/models` 元数据、健康检查等）
  - [x] 集成测覆盖所有 7 个协议端到端命中与放行
- **侵入面统计**：
  - 修改文件：`router/relay-router.go`（+3 行以内）
  - 新增文件：`middleware/content_moderation.go`
- **备注**：把审核前置到中间件层，比修改 controller 更优雅 —— controller 关注计费/转发主链路，安全审计在 middleware 层，符合关注点分离。代价是 body 要读两次（中间件读一次、controller 读一次），但 new-api 已有 `common.GetRequestBody` 缓存机制可复用。

## G13: Admin REST API（最小侵入注册）

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：实现 12 个 admin endpoints，覆盖配置管理、日志查询、黑名单管理、解封、preview、test-api-keys。**路由注册通过新增独立 router 文件 + api-router.go 单行调用实现**。
- **依赖**：G8, G10
- **侵入面统计**：
  - 修改文件：`router/api-router.go`（+1 行，调用 `router.RegisterContentModerationRoutes(apiRouter)`）
  - 新增文件：`controller/content_moderation.go`、`router/content_moderation_router.go`
- **验收标准**：
  - [x] `controller/content_moderation.go` 实现 12 个 handler：
    - `GET /api/admin/content_moderation/config`
    - `PUT /api/admin/content_moderation/config`
    - `GET /api/admin/content_moderation/status`
    - `POST /api/admin/content_moderation/test_api_keys`
    - `POST /api/admin/content_moderation/preview`（body: text + images）
    - `GET /api/admin/content_moderation/logs?user_id=&flagged=&start=&end=&page=`
    - `GET /api/admin/content_moderation/logs/:id`
    - `DELETE /api/admin/content_moderation/flagged_hash`（body: {hash}）
    - `GET /api/admin/content_moderation/flagged_hash/count`
    - `POST /api/admin/content_moderation/flagged_hash/clear`
    - `POST /api/admin/content_moderation/unban/:user_id`
    - `GET /api/admin/content_moderation/violation_count/:user_id`
  - [x] 路由注册到 `router/api-router.go` 的 admin 组，全部使用 `middleware.AdminAuth()`
  - [x] API Key 返回前端时脱敏（前 4 + `***` + 后 4）
  - [x] 单测/集成测：每个 endpoint 至少一个 happy path + 一个 401 unauthorized
- **备注**：endpoint 路径使用 snake_case 与 new-api 现有 admin API 风格一致。

## G14: 监控指标接入

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：把 7 个核心指标接入 `pkg/perf_metrics` 现有体系，让 new-api 的监控 endpoint 输出 CM 模块指标。
- **依赖**：G8, G9
- **验收标准**：
  - [x] 注册 7 个指标：
    - `content_moderation_requests_total{layer,mode,action}` counter
    - `content_moderation_latency_ms{layer}` histogram
    - `content_moderation_queue_length` gauge
    - `content_moderation_queue_dropped_total` counter
    - `content_moderation_workers_active` gauge
    - `content_moderation_openai_errors_total{type}` counter
    - `content_moderation_auto_bans_total` counter
  - [x] 在 service 关键路径埋点
  - [x] 验证 `GET /api/admin/perf_metrics`（或对应 endpoint）能看到新指标
  - [x] 单测覆盖埋点调用次数
- **备注**：先看 `pkg/perf_metrics` 现有用法再决定 label 设计。

## G15: 前端配置页面（Admin Config）

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：在 `web/default/src/` 新增 CM 配置页面，含全部配置项表单 + API Key 测试按钮 + 13 类阈值表格 + 模型范围 textarea。
- **依赖**：G13
- **侵入面统计**：
  - 修改文件：路由配置文件（+2-4 行新 path 注册）、菜单配置文件（+1 项菜单）
  - 新增文件：`web/default/src/pages/ContentModeration/Config.tsx` 及其拆分子组件、API 调用层 `web/default/src/services/contentModeration.ts`
- **验收标准**：
  - [x] 在"系统设置"或"操作设置"下新增 tab "内容审核"
  - [x] 表单分组：基础（enabled / mode / model_mode） / OpenAI（base_url / api_keys / model / timeout / retry）/ 阈值（13 类表格）/ 限制（sample_rate / model_list / input_scope / pre_hash_check） / 处置（block_status / block_message / auto_ban / email）/ 日志（retention）
  - [x] API Key 输入框配 "测试连通性" 按钮，调 `POST /test_api_keys`
  - [x] 阈值表格：每行 = 类别 + 数字输入框（step=0.01）+ "恢复默认"按钮；表头 "全部恢复默认"
  - [x] 模型范围：mode 下拉 + textarea + "从已有 channel 模型自动填充"按钮
  - [x] 表单提交后 toast 反馈，错误显示具体字段
  - [x] 类型安全：TypeScript 接口定义与后端 dto 对齐
- **备注**：组件复用 web/default 现有 Form / Table / Input 组件库（Base UI）。

## G16: 前端日志查询 + 黑名单 + 状态页面

- [x] **状态**：completed
- **工作量**：1 天
- **目标描述**：完成 CM 模块的另外 3 个管理页面：日志列表、黑名单管理、运行状态。
- **依赖**：G13, G14
- **侵入面统计**：
  - 修改文件：路由配置文件（+3 行新 path 注册，与 G15 合并 PR 一次性改完）
  - 新增文件：`web/default/src/pages/ContentModeration/Logs.tsx` / `Blacklist.tsx` / `Status.tsx` 及其拆分子组件
- **验收标准**：
  - [x] 日志列表页：分页表格，列 = 时间 / 用户 / 协议 / 模型 / 命中层 / 类别 / 得分 / 动作；筛选器 = user_id + flagged + 时间范围；行点击展开详情（含 category_scores 全量 + input_excerpt + 错误信息）
  - [x] 详情弹窗内提供"解封用户"快捷按钮（条件：auto_banned=true 且 user 当前 disabled）
  - [x] 黑名单管理页：显示当前 Set 大小 + 输入框删除单个 hash + "清空全部"按钮（含二次确认）
  - [x] 状态页：实时（5s 自动刷新）显示 worker / queue / 计数器，含一个近 24h 命中趋势的简单柱状图（命中数 by hour）
  - [x] 类型安全 + 错误处理
- **备注**：柱状图可用现有图表库（如 nivo / recharts，看 web/default/package.json 现有依赖）。

## G17: i18n 翻译

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：补齐中英文翻译，包括前端 UI 文案、后端拦截 message、邮件模板。
- **依赖**：G15, G16, G6, G7
- **侵入面统计**：
  - 修改文件：`i18n/zh.toml`、`i18n/en.toml`、`web/default/src/i18n/locales/zh.json`、`web/default/src/i18n/locales/en.json`，**全部为追加 key 不修改既有 key**
  - 新增文件：无
- **验收标准**：
  - [x] 后端：`i18n/zh.toml` 和 `i18n/en.toml` 新增 CM 模块所有 key（约 30-50 个）
  - [x] 前端：`web/default/src/i18n/locales/zh.json` 和 `en.json` 新增前端 UI 所有 key
  - [x] 切换 UI 语言后无 missing key 报警
  - [x] 邮件模板的中英文版本完整
- **备注**：fr / ru / ja / vi 留 fallback 到 en，社区翻译后续补。

## G18: 集成测套件（端到端验收）

- [x] **状态**：completed
- **工作量**：1.5 天
- **目标描述**：实现 overview.md 中列出的端到端验收场景 S1-S6 的自动化测试，跑通 CI。
- **依赖**：G1-G14
- **验收标准**：
  - [x] 集成测使用 SQLite + miniredis（or 实际 Redis docker）
  - [x] mock OpenAI Moderation 端（httptest.NewServer）
  - [x] 覆盖场景：
    - S1：enabled=false 时主流程零影响（基线对比）
    - S2：observe + 正常请求 100 条 → 全部通过 + 日志写入率 100%
    - S3：observe + 越狱 prompt 集 → 命中率与 sub2api 对齐（≤5% 差异）
    - S4：pre_block + 同一集合 → 命中拦截，三协议格式正确
    - S5：连续 10 次违规 → 自动封禁 + user.status 更新 + 邮件 mock 收到
    - S6：删除黑名单 hash → 后续相同输入重新走 OpenAI
    - S7：模型 whitelist 测试（仅 gpt-4o 审核，其他跳过）
    - S8：fail-open（OpenAI 5xx 全失败）→ 主链路放行
  - [x] CI 集成测在 `go test ./...` 全绿
- **备注**：越狱 prompt 集合从 sub2api 同步过来，存 testdata。

## G19: 文档

- [x] **状态**：completed
- **工作量**：0.5 天
- **目标描述**：写运维与使用文档。
- **依赖**：G1-G18
- **验收标准**：
  - [x] `README.md` 添加内容审核模块章节（功能概述 + 启用方式 + 链接到详细文档）
  - [x] `docs/content_moderation.md` 新增，含：
    - 架构概览（两层串联图）
    - 配置项说明（每个 option 的含义、默认值、调优建议）
    - 灰度上线 SOP（observe → pre_block 的标准流程）
    - 回滚预案
    - 13 类阈值调优指南（每类含义、调高/调低后果）
    - 模型选择性审核使用示例
    - 常见 FAQ（Redis 故障怎么办、误判怎么修正、自动封禁如何撤销等）
    - 监控指标说明
  - [x] README 的英文版同步更新（README.en.md 等）
- **备注**：文档先写中文，英文版可后续翻译。

---

## 子目标依赖图

```
G1 (配置 + 表)
 ├─→ G2 (输入提取) ──→ G8 (Service 编排) ──→ G9 (Worker)
 ├─→ G3 (OpenAI 客户端) ──┤                      └─→ G12 (钩子)
 ├─→ G4 (Hash 缓存) ──────┤
 ├─→ G5 (计数器) ─────────┼──→ G11 (自动封禁) ──→ G18 (集成测)
 ├─→ G6 (邮件) ───────────┤                            ↑
 ├─→ G7 (响应适配) ───────┤                            │
 └─→ G10 (日志持久化) ────┘                            │
                                G13 (Admin API) ────→ G15 (前端配置) ──→ G16 (前端日志)
                                                                  ↓
                                G14 (监控指标) ──────────────────→ G17 (i18n)
                                                                  ↓
                                                                 G19 (文档)
```

可并行：G2 / G3 / G4 / G5 / G6 / G7 / G10 全部独立，可同时推进。

总工作量估计：~12 天（不计 buffer）。
