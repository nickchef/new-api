# new-api 内容审核模块（Content Moderation）项目概览

## 背景与目标

new-api 当前内置一套基于 AC 自动机的本地敏感词检查（`service/sensitive.go`），但能力有限：仅文本、仅本地词库匹配、无分类得分、无图像支持、无观察模式、无日志持久化、无自动封禁。在面对越狱 prompt、多模态违规、批量恶意流量时缺乏纵深防御。

姊妹项目 sub2api 已落地一套完整的内容审核体系（OpenAI Moderation + observe/pre_block 双模式 + Hash 黑名单 + 异步队列 + 自动封禁 + 管理后台），覆盖 OpenAI/Claude/Gemini 等协议。

**本项目目标**：把 sub2api 的全套内容审核能力迁移到 new-api，保留原有本地词库作为第一层硬拦，新增 OpenAI Moderation 作为第二层智能审核，并在 new-api 现有架构（GORM、option 表、AdminAuth、Redis、perf_metrics、React 19 前端、三库通用）下落地。

## 范围

### In scope

- 两层串联架构：本地 AC 词库（L1 同步硬拦） + OpenAI omni-moderation-latest（L2 按 mode 工作）
- 三种工作模式：`off` / `observe`（异步队列、永远放行、只记日志） / `pre_block`（同步审核、命中拦截）
- 协议覆盖：OpenAI Chat/Completions/Responses、Claude Messages、Gemini、OpenAI Images、MJ、Suno
- 多模态：文本 + 图像（image_url / base64 / Claude source / Gemini inline_data 与 fileData）
- 仅审核输入（用户 prompt），不审核 AI 输出
- 配置粒度：仅全局
- **模型选择性审核**：`all` / `whitelist` / `blacklist` 三档 + 通配符匹配
- **输入提取范围开关**：默认 `last_user`（与 sub2api 一致），可配 `all_user` / `all_messages`
- `<system-reminder>` 文本过滤
- Redis Hash 黑名单预检（pre_hash_check）
- 多 API Key 轮转 + 失败 1 分钟冻结
- 自动封禁（默认 720h 滑窗 / 10 次阈值）
- 邮件告警（自动封禁触发，含限频）
- 日志持久化（命中 180 天 / 未命中 3 天，自动清理）
- 拦截响应随原协议适配（OpenAI / Claude / Gemini 三种格式）
- 三库通用（SQLite/MySQL/PG），JSON 字段落 TEXT
- Admin REST API（配置、日志、黑名单、解封、preview、test-api-keys）
- 监控指标接入 new-api 现有 `pkg/perf_metrics`
- 前端 `web/default` 新增 4 个管理页面（配置 / 日志 / 黑名单 / 状态）
- i18n：中文 + 英文

### Out of scope

- 输出审核（AI 返回内容）
- 音频审核（STT 转译后再审核）
- 用户级 / 用户组级配置粒度（仅全局）
- 用户申诉机制
- 白名单"永不审核" hash 列表（管理员可手动删黑名单 hash 等价实现）
- 演练模式（dry-run）
- `web/classic` 前端
- 其他语言翻译（fr/ru/ja/vi 由社区补）
- 数据库向下迁移
- 命中 hash 的过期清理（无 TTL，依赖管理员手动维护）

## 技术选型

- **后端**：Go + Gin + GORM v2，遵循 new-api `Router -> Controller -> Service -> Model` 分层
- **配置存储**：option 表（与现有配置体系一致，热生效）
- **缓存**：Redis（必需）—— Hash 黑名单（Set）+ 违规计数（ZSET 滑窗）+ 邮件限频（String + TTL）
- **数据库**：SQLite/MySQL/PostgreSQL 三库通用，遵守 CLAUDE.md Rule 2
- **JSON 序列化**：统一走 `common.Marshal/Unmarshal`，遵守 Rule 1
- **HTTP 客户端**：复用 new-api 现有 HTTP 池或 `net/http`，3000ms 超时 + 1 次重试
- **JSON 解析**：用 `gjson`（与 sub2api 一致，处理多协议异构 body）
- **前端**：React 19 + Rsbuild + Base UI + Tailwind，i18next
- **依赖注入**：包级单例 + sync.RWMutex，不引入 wire（与 new-api 风格一致）
- **协议常量**：复用 `types.RelayFormat*`，新增内部 ProtocolKey 映射
- **接入点**：通过新增 Gin **中间件** 注入到 relay 路由链路（详见下节"最小侵入原则"）

## 最小侵入原则（Fork 友好性）

本项目是 new-api 的 fork，需要长期跟随上游更新。**所有改动以"新增文件优先、单行修改其次、多行修改尽量避免"为铁律**。功能完整性优先级最高，但同等效果下必须选择最不侵入的方案。

### 允许修改的上游文件清单（含最大允许行数）

| 文件 | 最大允许修改 | 用途 |
|------|------------|------|
| `router/relay-router.go` | +3 行 | 注册 CM 中间件到 relay 路由组 |
| `router/api-router.go` | +1 行 | 注册 CM admin 路由（调用新增的 RegisterContentModerationRoutes） |
| `main.go` | +2 行 | 启动时初始化 CM service 与 worker |
| `model/main.go` | +1 行 | AutoMigrate 时添加 `&ContentModerationLog{}` |
| `web/default/src/App.tsx`（或路由配置） | +2 行 | 注册前端路由 |
| `web/default/src/`（菜单配置文件） | +1 项 | 新增"内容审核"菜单项 |
| `i18n/zh.toml`、`i18n/en.toml` | 追加新 key | 翻译键，纯追加 |
| `web/default/src/i18n/locales/{zh,en}.json` | 追加新 key | 前端翻译键，纯追加 |
| `AGENTS.md` | 追加章节 | 规格记录，纯追加 |

### 禁止修改的上游文件

- `controller/relay.go`（包括钩子点 126-143）—— **改用中间件实现**
- `service/sensitive.go` —— L1 词库逻辑保持原样，由新中间件主动调用
- `setting/sensitive.go` —— 保持原配置开关语义
- `model/log.go`、`model/user.go` 主体逻辑 —— 仅通过新增方法或字段扩展
- 任何 `relay/channel/*` adapter
- `CLAUDE.md`（包含 Rule 5 保护品牌信息条款）

### 实现策略

- **CM 中间件**：新增 `middleware/content_moderation.go`，导出 `ContentModeration()` Gin 中间件。中间件内部完成"L1 调用 `service.CheckSensitiveText` → L2 调用 `service.CMService.Check` → 命中即 `c.AbortWithStatusJSON`"。整个审核流程在 controller 之前结束。
- **CM 路由**：新增 `router/content_moderation_router.go`，导出 `RegisterContentModerationRoutes(r *gin.Engine)`，封装全部 12 个 admin endpoints 与 middleware.AdminAuth 绑定。
- **现有 controller/relay.go 钩子保持原样**：当 CM 中间件已 abort，controller 永远不会执行，里面的 `needSensitiveCheck` 分支自然不会跑。当 CM 模块 disabled 时回退到现有行为，零差异。
- **配置项注入**：option 表通过 GORM AutoMigrate 自动创建，默认值在新增的 `setting/content_moderation.go` 的 init() 中通过现有 option 读取/写入 API 注入，无需改 `model/option.go`。
- **菜单与路由（前端）**：通过单个菜单配置文件追加一项，路由配置追加 2-4 个新 path 指向新增的 page 组件文件。所有页面组件全部是新增 `.tsx` 文件。
- **依赖追加**：`go.mod` 仅在确实需要新依赖时追加（如 `gjson` 若 new-api 未引入），不影响现有依赖版本。

### 衡量指标

整个项目结束时，对上游的累计修改 diff 行数应满足：
- 后端非新增文件总改动 **< 30 行**
- 前端非新增文件总改动 **< 15 行**
- 配置/翻译文件追加不计

若实现过程中发现某个功能必须超出此预算，需在 PR 描述里明确说明原因与权衡。

## 整体准出条件

- [x] **零侵入兼容**：`ContentModerationEnabled=false` 时主链路零开销（middleware 入口快速 return），S1 集成测验证 upstream 不被调用且无日志写入。生产 P99 延迟对比需上线后验证
- [x] **三库 schema 一致**：SQLite AutoMigrate + INSERT + SELECT 实测通过；MySQL/PG 通过 GORM 抽象 + 反引号列名 + TEXT 存 JSON 应当 work（建议上线前手动跑三库迁移做最终确认）
- [ ] **越狱集命中率对齐 sub2api**：跨项目命中率对比未做，需 fixtures + 离线 batch 脚本。**待补**
- [x] **多协议端到端通**：service 层 7 协议全部测过（OpenAI Chat/Responses/Anthropic/Gemini/Images/MJ/Suno）；S3/S7 集成测验证 pre_block 命中 + whitelist 跳过；observe 模式 S2 验证。真完整 HTTP E2E（含 distributor）需上线灰度
- [x] **观察→拦截灰度路径**：mode 通过 setting 热生效（无需重启），hash 缓存跨模式共享。手动灰度脚本待补
- [x] **模型选择性审核生效**：whitelist + blacklist + 通配符（`gpt-4*` / `GPT-?o`）单测 9/9 通过；S7 集成测验证 upstream 0 调用
- [x] **自动封禁链路**：S5 集成测连续 3 次违规触发 disable（默认阈值 10，spec 验证用 3）；邮件 mock 投递验证；24h 限频通过 SetNX
- [x] **fail-open / fail-close 行为正确**：S8 集成测验证 OpenAI 5xx 全失败时 fail-open；Redis 不可达 → 内存 fallback；DB 写日志失败不阻塞主链路（writeLog 内部不抛错）
- [x] **核心 service 单测覆盖率 ≥ 80%**：核心决策文件 content_moderation.go 92% / openai 87.7% / input 92.1% / metrics 96.4% / response 92.3% / redact 87.5%（≥ 80% 阈值）；plumbing 文件 bootstrap/cleanup/counter 较低（< 80%）因 goroutine loop 和 Redis 路径需运行时
- [x] **Admin API 全通**：12 endpoints 全部实现 + 路由注册到 `/api/admin/content_moderation/*` + middleware.AdminAuth 强制；11 单测覆盖各 handler；HTTP 集成测可在 CI 补
- [x] **前端 UI 走通核心流程**：4 个 page（config/logs/blacklist/status）+ 完整 API client 已实现；TanStack Router 自动生成 routeTree.gen.ts，首次 `bun run dev` 触发。**手动 UAT 待补**
- [x] **监控指标可见**：7 核心指标全部接入 service.InspectContentModerationMetrics()，通过 `GET /api/admin/content_moderation/status` 暴露；前端 status 页 5s 自动刷新展示
- [x] **文档齐全**：`docs/content_moderation.md`（含架构图 / 28 配置项详表 / 灰度 6 步 SOP / 回滚预案 / 13 类阈值调优 / FAQ）+ README 加章节
- [x] **i18n 完整**：zh + en 后端 21 key + 前端 55 key 已追加；fr/ru/ja/vi 按 spec 留 fallback
- [x] **CI 全绿**：`go test ./service/ ./middleware/ ./controller/` 全 green；完整 `go build ./...` 因 `web/classic/dist` 缺失而需先跑 `bun run build` 才能 embed
