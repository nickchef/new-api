package service

import (
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationCheckRequest 是中间件传入 service 的请求快照。
type ContentModerationCheckRequest struct {
	RequestID string
	UserID    int
	Username  string
	TokenID   int
	TokenName string
	Group     string
	IP        string
	Endpoint  string
	Provider  string
	Model     string
	Protocol  string
	Body      []byte
}

// ContentModerationCheckResult 是 Check 主入口的返回。Action 决定中间件后续动作。
type ContentModerationCheckResult struct {
	Action          string // allow / block / hash_block / error
	DetectionLayer  string
	HighestCategory string
	HighestScore    float64
	CategoryScores  map[string]float64
	InputText       string
	InputHash       string
	Excerpt         string
	UpstreamLatencyMS int
	Error           string
	Flagged         bool
}

// ContentModerationLogSink 由调用方注入。中间件用 model.CreateContentModerationLog 实现，
// 单测可以注入内存收集器。
// 字段命名对齐 model.ContentModerationLog。
type ContentModerationLogSink interface {
	Write(ctx context.Context, log ContentModerationLogRecord)
}

// ContentModerationLogRecord 是落库前的中间表示。这里故意不引用 model 包以避免循环依赖。
type ContentModerationLogRecord struct {
	RequestID         string
	UserID            int
	Username          string
	TokenID           int
	TokenName         string
	Group             string
	IP                string
	Endpoint          string
	Provider          string
	Model             string
	Protocol          string
	Mode              string
	Action            string
	DetectionLayer    string
	Flagged           bool
	HighestCategory   string
	HighestScore      float64
	CategoryScores    map[string]float64
	ThresholdSnapshot map[string]float64
	InputExcerpt      string
	InputHash         string
	UpstreamLatencyMs int
	QueueDelayMs      int
	Error             string
	ViolationCount    int
	AutoBanned        bool
	EmailSent         bool
	CreatedAt         int64
}

// ContentModerationService 是 CM 主入口。包级单例 CMService。
type ContentModerationService struct {
	logSink         ContentModerationLogSink
	enqueueObserve  func(ContentModerationCheckRequest)
	autoBanCallback func(ctx context.Context, userID int, rec *ContentModerationLogRecord)
}

// CMService 是包级共享单例。生产环境由 main.go 调用 InitContentModerationService 注入。
var CMService = &ContentModerationService{}

// InitContentModerationService 注入运行依赖。Worker / Sink / 自动封禁回调由调用方提供。
func InitContentModerationService(
	sink ContentModerationLogSink,
	enqueueObserve func(ContentModerationCheckRequest),
	autoBanCallback func(ctx context.Context, userID int, rec *ContentModerationLogRecord),
) {
	CMService.logSink = sink
	CMService.enqueueObserve = enqueueObserve
	CMService.autoBanCallback = autoBanCallback
}

// Check 是 CM 主入口。返回 ContentModerationCheckResult.Action 决定后续：
//
//   - allow      → c.Next()
//   - block      → 中间件用 G7 适配器 AbortWithStatusJSON
//   - hash_block → 同 block，区别仅在日志层
//   - error      → fail-open，但写 error 日志
func (s *ContentModerationService) Check(ctx context.Context, req ContentModerationCheckRequest) ContentModerationCheckResult {
	cfg := setting.GetContentModerationSetting()
	result := ContentModerationCheckResult{Action: setting.ContentModerationActionAllow}

	// 1) 模块禁用
	if !cfg.Enabled || cfg.Mode == setting.ContentModerationModeOff {
		return result
	}

	// 2) 模型范围检查
	if !setting.ContentModerationModelInScope(req.Model) {
		return result
	}

	// 3) 输入提取
	input := ExtractContentModerationInput(req.Protocol, req.Body, cfg.InputScope)
	if input.IsEmpty() {
		return result
	}
	result.InputText = input.Text
	result.InputHash = input.Hash
	result.Excerpt = buildContentModerationExcerpt(input.Text)

	// 4) Hash 预检
	if cfg.PreHashCheckEnabled && CMHashCache != nil {
		if hit, err := CMHashCache.Has(ctx, input.Hash); err == nil && hit {
			result.Flagged = true
			result.Action = setting.ContentModerationActionHashBlock
			result.DetectionLayer = setting.ContentModerationLayerOpenAI
			s.writeLog(ctx, cfg.Mode, req, result, 0)
			s.applyFlaggedSideEffects(ctx, cfg, req, &result)
			return result
		}
	}

	// 5) 采样：相同 hash 决策稳定（哈希取模，避免对相同输入忽采忽不采）
	if cfg.SampleRate > 0 && cfg.SampleRate < 100 {
		if hashedMod(input.Hash, 100) >= cfg.SampleRate {
			return result
		}
	}

	// 6) observe 模式：入队 + 直接 allow
	if cfg.Mode == setting.ContentModerationModeObserve {
		if s.enqueueObserve != nil {
			s.enqueueObserve(req)
		}
		return result
	}

	// 7) pre_block 模式：同步审核
	return s.checkSync(ctx, cfg, req, input, true)
}

// CheckObserveAsync 是 worker 调用的同步审核（永不拦截，仅写日志）。
func (s *ContentModerationService) CheckObserveAsync(ctx context.Context, req ContentModerationCheckRequest) {
	cfg := setting.GetContentModerationSetting()
	if !cfg.Enabled {
		return
	}
	if !setting.ContentModerationModelInScope(req.Model) {
		return
	}
	input := ExtractContentModerationInput(req.Protocol, req.Body, cfg.InputScope)
	if input.IsEmpty() {
		return
	}
	res := s.checkSync(ctx, cfg, req, input, false)
	_ = res
}

func (s *ContentModerationService) checkSync(
	ctx context.Context,
	cfg setting.ContentModerationSetting,
	req ContentModerationCheckRequest,
	input ContentModerationInput,
	allowBlock bool,
) ContentModerationCheckResult {
	result := ContentModerationCheckResult{
		Action:    setting.ContentModerationActionAllow,
		InputText: input.Text,
		InputHash: input.Hash,
		Excerpt:   buildContentModerationExcerpt(input.Text),
	}

	apiResult, err := CMClient.Call(ctx, cfg, input.Text, input.Images)
	if err != nil {
		// fail-open：API 不可用不阻塞主链路，但写 error 日志
		result.Action = setting.ContentModerationActionError
		result.Error = err.Error()
		if errors.Is(err, ErrCMAllKeysFrozen) || errors.Is(err, ErrCMNoConfiguredKey) {
			result.Action = setting.ContentModerationActionAllow
		}
		RecordContentModerationOpenAIError(classifyContentModerationError(err))
		s.writeLog(ctx, cfg.Mode, req, result, 0)
		RecordContentModerationRequest(setting.ContentModerationLayerOpenAI, cfg.Mode, result.Action)
		return result
	}
	RecordContentModerationOpenAILatency(apiResult.LatencyMS)
	result.UpstreamLatencyMS = apiResult.LatencyMS
	result.HighestCategory = apiResult.HighestCategory
	result.HighestScore = apiResult.HighestScore
	result.CategoryScores = apiResult.CategoryScores
	result.Flagged = apiResult.Flagged
	result.DetectionLayer = setting.ContentModerationLayerOpenAI

	if !apiResult.Flagged {
		// 未命中，按 cfg.RecordNonHits 决定是否落库
		if cfg.RecordNonHits {
			s.writeLog(ctx, cfg.Mode, req, result, 0)
		}
		return result
	}

	// 命中
	result.Action = setting.ContentModerationActionBlock
	if !allowBlock {
		result.Action = setting.ContentModerationActionAllow // observe 模式不拦截
	}

	// 写入 Hash 黑名单缓存
	if CMHashCache != nil {
		_ = CMHashCache.Record(ctx, input.Hash)
	}
	s.writeLog(ctx, cfg.Mode, req, result, 0)
	s.applyFlaggedSideEffects(ctx, cfg, req, &result)
	RecordContentModerationRequest(setting.ContentModerationLayerOpenAI, cfg.Mode, result.Action)
	return result
}

// classifyContentModerationError 把上游错误归类，给指标用。
func classifyContentModerationError(err error) string {
	if err == nil {
		return "other"
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "401") || strings.Contains(msg, "unauthorized"):
		return "auth"
	case strings.Contains(msg, "429") || strings.Contains(msg, "rate"):
		return "rate_limit"
	case strings.Contains(msg, "deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "timeout"
	default:
		return "other"
	}
}

func (s *ContentModerationService) writeLog(
	ctx context.Context,
	mode string,
	req ContentModerationCheckRequest,
	res ContentModerationCheckResult,
	queueDelayMs int,
) {
	if s == nil || s.logSink == nil {
		return
	}
	cfg := setting.GetContentModerationSetting()
	rec := ContentModerationLogRecord{
		RequestID:         req.RequestID,
		UserID:            req.UserID,
		Username:          req.Username,
		TokenID:           req.TokenID,
		TokenName:         req.TokenName,
		Group:             req.Group,
		IP:                req.IP,
		Endpoint:          req.Endpoint,
		Provider:          req.Provider,
		Model:             req.Model,
		Protocol:          req.Protocol,
		Mode:              mode,
		Action:            res.Action,
		DetectionLayer:    res.DetectionLayer,
		Flagged:           res.Flagged,
		HighestCategory:   res.HighestCategory,
		HighestScore:      res.HighestScore,
		CategoryScores:    res.CategoryScores,
		ThresholdSnapshot: cfg.Thresholds,
		InputExcerpt:      res.Excerpt,
		InputHash:         res.InputHash,
		UpstreamLatencyMs: res.UpstreamLatencyMS,
		QueueDelayMs:      queueDelayMs,
		Error:             res.Error,
		CreatedAt:         time.Now().Unix(),
	}
	s.logSink.Write(ctx, rec)
}

func (s *ContentModerationService) applyFlaggedSideEffects(
	ctx context.Context,
	cfg setting.ContentModerationSetting,
	req ContentModerationCheckRequest,
	res *ContentModerationCheckResult,
) {
	if s == nil || res == nil || !res.Flagged || req.UserID <= 0 || s.autoBanCallback == nil {
		return
	}
	rec := ContentModerationLogRecord{
		RequestID:       req.RequestID,
		UserID:          req.UserID,
		Username:        req.Username,
		HighestCategory: res.HighestCategory,
		CreatedAt:       time.Now().Unix(),
	}
	s.autoBanCallback(ctx, req.UserID, &rec)
}

// buildContentModerationExcerpt 截断长度 + PII 脱敏。
//
// PII 规则保守：邮箱 / 手机号 / 16-19 位数字串。命中后整段替换为 <PII>。
func buildContentModerationExcerpt(text string) string {
	excerpt := redactContentModerationPII(text)
	excerpt = trimUnicodeRunes(excerpt, setting.MaxContentModerationExcerptRunes)
	return excerpt
}

// hashedMod 用 hash 字符串的前 8 字节做模运算，对相同 hash 结果稳定。
func hashedMod(hash string, mod int) int {
	if mod <= 0 {
		return 0
	}
	if len(hash) < 16 {
		hash = strings.Repeat("0", 16-len(hash)) + hash
	}
	// hash 是 hex 字符串，每两个字符表示一个字节。取前 8 字节（16 hex char）转 uint64。
	var buf [8]byte
	for i := 0; i < 8; i++ {
		hi := hexNibble(hash[2*i])
		lo := hexNibble(hash[2*i+1])
		buf[i] = byte(hi<<4 | lo)
	}
	n := binary.BigEndian.Uint64(buf[:])
	return int(n % uint64(mod))
}

func hexNibble(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return 0
	}
}

// LookupRequestID 是辅助函数，从 gin context 中读 request_id（如未来需要）。
// 这里保留为占位，避免 service 包对 gin 强依赖。
var _ = common.GetRandomString
