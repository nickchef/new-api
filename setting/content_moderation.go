package setting

import (
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/setting/config"
)

// Content Moderation 工作模式
const (
	ContentModerationModeOff      = "off"
	ContentModerationModeObserve  = "observe"
	ContentModerationModePreBlock = "pre_block"
)

// 模型选择性审核策略
const (
	ContentModerationModelModeAll       = "all"
	ContentModerationModelModeWhitelist = "whitelist"
	ContentModerationModelModeBlacklist = "blacklist"
)

// 输入提取范围
const (
	ContentModerationInputScopeLastUser    = "last_user"
	ContentModerationInputScopeAllUser     = "all_user"
	ContentModerationInputScopeAllMessages = "all_messages"
)

// Action 决策结果
const (
	ContentModerationActionAllow     = "allow"
	ContentModerationActionBlock     = "block"
	ContentModerationActionHashBlock = "hash_block"
	ContentModerationActionError     = "error"
)

// DetectionLayer 命中层
const (
	ContentModerationLayerLocal   = "local_keyword"
	ContentModerationLayerOpenAI  = "openai_moderation"
)

// 内部默认值
const (
	DefaultContentModerationBaseURL              = "https://api.openai.com"
	DefaultContentModerationModel                = "omni-moderation-latest"
	DefaultContentModerationTimeoutMS            = 3000
	MaxContentModerationTimeoutMS                = 30000
	MinContentModerationTimeoutMS                = 1000
	DefaultContentModerationRetryCount           = 1
	MaxContentModerationRetryCount               = 5
	DefaultContentModerationWorkerCount          = 4
	MaxContentModerationWorkerCount              = 32
	DefaultContentModerationQueueSize            = 32768
	MaxContentModerationQueueSize                = 100000
	DefaultContentModerationBanThreshold         = 10
	DefaultContentModerationViolationWindowHours = 720
	DefaultContentModerationBlockStatus          = 403
	DefaultContentModerationBlockMessage         = "Your input was blocked by content moderation policy. Please revise and retry."
	DefaultContentModerationHitRetentionDays     = 180
	DefaultContentModerationNonHitRetentionDays  = 3
	MaxContentModerationRetentionDays            = 3650
	DefaultContentModerationSampleRate           = 100 // 0-100; 100 = always
	MaxContentModerationInputRunes               = 12000
	MaxContentModerationExcerptRunes             = 240
	MaxContentModerationInputImages              = 8
)

// Content Moderation 13 类别
var contentModerationCategoryOrder = []string{
	"harassment",
	"harassment/threatening",
	"hate",
	"hate/threatening",
	"illicit",
	"illicit/violent",
	"self-harm",
	"self-harm/intent",
	"self-harm/instructions",
	"sexual",
	"sexual/minors",
	"violence",
	"violence/graphic",
}

// ContentModerationCategories 返回 13 类别的有序副本
func ContentModerationCategories() []string {
	out := make([]string, len(contentModerationCategoryOrder))
	copy(out, contentModerationCategoryOrder)
	return out
}

// ContentModerationDefaultThresholds 13 类阈值默认值
func ContentModerationDefaultThresholds() map[string]float64 {
	return map[string]float64{
		"harassment":             0.98,
		"harassment/threatening": 0.90,
		"hate":                   0.65,
		"hate/threatening":       0.65,
		"illicit":                0.95,
		"illicit/violent":        0.95,
		"self-harm":              0.65,
		"self-harm/intent":       0.85,
		"self-harm/instructions": 0.65,
		"sexual":                 0.65,
		"sexual/minors":          0.65,
		"violence":               0.95,
		"violence/graphic":       0.95,
	}
}

// ContentModerationSetting 是 CM 模块的全部配置项。所有字段都通过 config.GlobalConfig
// 序列化到 option 表，热生效。
type ContentModerationSetting struct {
	// 基础开关
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"`

	// OpenAI Moderation 调用参数
	BaseURL    string   `json:"base_url"`
	Model      string   `json:"model"`
	APIKeys    []string `json:"api_keys"`
	TimeoutMS  int      `json:"timeout_ms"`
	RetryCount int      `json:"retry_count"`

	// 阈值（13 类）
	Thresholds map[string]float64 `json:"thresholds"`

	// 输入限制
	SampleRate          int    `json:"sample_rate"`
	InputScope          string `json:"input_scope"`
	PreHashCheckEnabled bool   `json:"pre_hash_check_enabled"`

	// 模型选择性审核
	ModelMode string   `json:"model_mode"`
	ModelList []string `json:"model_list"`

	// 处置
	BlockStatus  int    `json:"block_status"`
	BlockMessage string `json:"block_message"`

	// 自动封禁
	AutoBanEnabled       bool `json:"auto_ban_enabled"`
	BanThreshold         int  `json:"ban_threshold"`
	ViolationWindowHours int  `json:"violation_window_hours"`

	// 邮件告警
	EmailOnHit   bool `json:"email_on_hit"`
	EmailToAdmin bool `json:"email_to_admin"`
	EmailToUser  bool `json:"email_to_user"`

	// 异步队列
	WorkerCount int `json:"worker_count"`
	QueueSize   int `json:"queue_size"`

	// 日志
	RecordNonHits       bool `json:"record_non_hits"`
	HitRetentionDays    int  `json:"hit_retention_days"`
	NonHitRetentionDays int  `json:"non_hit_retention_days"`

	// observe 模式下是否仍调用 OpenAI（observe 默认照常调用，关闭则只走 L1 词库）
	ObserveSendHitsToOpenAI bool `json:"observe_send_hits_to_openai"`
}

var (
	contentModerationSetting = newDefaultContentModerationSetting()
	contentModerationMu      sync.RWMutex
)

func newDefaultContentModerationSetting() ContentModerationSetting {
	return ContentModerationSetting{
		Enabled:                 false,
		Mode:                    ContentModerationModeOff,
		BaseURL:                 DefaultContentModerationBaseURL,
		Model:                   DefaultContentModerationModel,
		APIKeys:                 []string{},
		TimeoutMS:               DefaultContentModerationTimeoutMS,
		RetryCount:              DefaultContentModerationRetryCount,
		Thresholds:              ContentModerationDefaultThresholds(),
		SampleRate:              DefaultContentModerationSampleRate,
		InputScope:              ContentModerationInputScopeLastUser,
		PreHashCheckEnabled:     true,
		ModelMode:               ContentModerationModelModeAll,
		ModelList:               []string{},
		BlockStatus:             DefaultContentModerationBlockStatus,
		BlockMessage:            DefaultContentModerationBlockMessage,
		AutoBanEnabled:          true,
		BanThreshold:            DefaultContentModerationBanThreshold,
		ViolationWindowHours:    DefaultContentModerationViolationWindowHours,
		EmailOnHit:              true,
		EmailToAdmin:            true,
		EmailToUser:             false,
		WorkerCount:             DefaultContentModerationWorkerCount,
		QueueSize:               DefaultContentModerationQueueSize,
		RecordNonHits:           false,
		HitRetentionDays:        DefaultContentModerationHitRetentionDays,
		NonHitRetentionDays:     DefaultContentModerationNonHitRetentionDays,
		ObserveSendHitsToOpenAI: true,
	}
}

func init() {
	config.GlobalConfig.Register("content_moderation", &contentModerationSetting)
}

// GetContentModerationSetting 返回当前配置的快照拷贝（线程安全）。
// 切片/map 字段为浅拷贝；调用方不得修改返回值。
func GetContentModerationSetting() ContentModerationSetting {
	contentModerationMu.RLock()
	defer contentModerationMu.RUnlock()
	return contentModerationSetting
}

// MutableContentModerationSetting 返回内部指针以便管理 API 写入。仅供管理后台使用。
func MutableContentModerationSetting() *ContentModerationSetting {
	return &contentModerationSetting
}

// LockContentModeration / UnlockContentModeration 为后台 PUT 配置时使用。
func LockContentModeration()   { contentModerationMu.Lock() }
func UnlockContentModeration() { contentModerationMu.Unlock() }

// IsContentModerationEnabled 是否启用 CM 模块。
func IsContentModerationEnabled() bool {
	cfg := GetContentModerationSetting()
	return cfg.Enabled && cfg.Mode != ContentModerationModeOff
}

// IsContentModerationPreBlock 是否处于同步拦截模式。
func IsContentModerationPreBlock() bool {
	cfg := GetContentModerationSetting()
	return cfg.Enabled && cfg.Mode == ContentModerationModePreBlock
}

// ContentModerationModelInScope 按 ModelMode + ModelList + 通配符规则判定 model 是否需要审核。
//
//   - ModelMode = "all"        → 所有模型都审
//   - ModelMode = "whitelist"  → 仅当 model 命中 ModelList 中任一通配符时才审
//   - ModelMode = "blacklist"  → 命中 ModelList 中任一通配符则跳过审核
//
// 通配符：`*` 匹配任意字符序列，`?` 匹配单字符。匹配为忽略大小写。
func ContentModerationModelInScope(model string) bool {
	cfg := GetContentModerationSetting()
	return contentModerationModelInScopeWith(cfg, model)
}

func contentModerationModelInScopeWith(cfg ContentModerationSetting, model string) bool {
	switch cfg.ModelMode {
	case ContentModerationModelModeWhitelist:
		return contentModerationMatchAny(cfg.ModelList, model)
	case ContentModerationModelModeBlacklist:
		return !contentModerationMatchAny(cfg.ModelList, model)
	default:
		return true
	}
}

func contentModerationMatchAny(patterns []string, model string) bool {
	if model == "" {
		return false
	}
	low := strings.ToLower(model)
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if contentModerationWildcardMatch(strings.ToLower(p), low) {
			return true
		}
	}
	return false
}

// contentModerationWildcardMatch 实现 `*` 与 `?` 通配符匹配，迭代法，无回溯，O(n*m)。
func contentModerationWildcardMatch(pattern, s string) bool {
	pi, si := 0, 0
	star, mark := -1, 0
	for si < len(s) {
		if pi < len(pattern) && (pattern[pi] == '?' || pattern[pi] == s[si]) {
			pi++
			si++
			continue
		}
		if pi < len(pattern) && pattern[pi] == '*' {
			star = pi
			mark = si
			pi++
			continue
		}
		if star != -1 {
			pi = star + 1
			mark++
			si = mark
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}
