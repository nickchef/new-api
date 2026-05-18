package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationKeyStatus 记录单个 OpenAI Moderation Key 的运行时状态。
type ContentModerationKeyStatus struct {
	Index          int
	Masked         string
	Healthy        bool
	FailureCount   int
	SuccessCount   int64
	LastError      string
	LastLatencyMS  int
	LastHTTPStatus int
	LastTestedAt   *time.Time
	FrozenUntil    *time.Time
}

// ContentModerationAPIResult 是 callModeration 的返回。CategoryScores 由 OpenAI 给出，
// Flagged / HighestCategory / HighestScore 由阈值评估得到。
type ContentModerationAPIResult struct {
	APIKeyMasked    string
	CategoryScores  map[string]float64
	Flagged         bool
	HighestCategory string
	HighestScore    float64
	LatencyMS       int
	HTTPStatus      int
}

// 错误常量
var (
	ErrCMAllKeysFrozen     = errors.New("content moderation: no api key available")
	ErrCMNoConfiguredKey   = errors.New("content moderation: no api key configured")
	ErrCMEmptyResponse     = errors.New("content moderation: empty results")
	errModerationBadStatus = errors.New("content moderation: upstream bad status")
)

// CMClient 单例。worker / middleware 共用同一份 Key 状态，避免重复冻结。
var (
	CMClient     = newContentModerationClient(nil)
	cmClientOnce sync.Once
)

type contentModerationKeyState struct {
	key            string
	masked         string
	failureCount   int
	successCount   int64
	frozenUntil    time.Time
	lastError      string
	lastLatencyMS  int
	lastHTTPStatus int
	lastTestedAt   *time.Time
}

// contentModerationClient 封装多 Key 轮转、冻结、调用。
type contentModerationClient struct {
	httpClient *http.Client

	mu         sync.Mutex
	keyStates  map[string]*contentModerationKeyState // key string -> state
	roundRobin int                                   // 轮转游标
}

func newContentModerationClient(client *http.Client) *contentModerationClient {
	if client == nil {
		client = &http.Client{}
	}
	return &contentModerationClient{
		httpClient: client,
		keyStates:  map[string]*contentModerationKeyState{},
	}
}

// InitContentModerationClient 在 main.go 启动时调用，注入共享 *http.Client。
func InitContentModerationClient(client *http.Client) {
	cmClientOnce.Do(func() {
		if client == nil {
			client = &http.Client{}
		}
		CMClient = newContentModerationClient(client)
	})
}

// Call 调用 OpenAI Moderation，按 cfg 内的 APIKeys 做轮转 + 冻结 + 重试。
func (c *contentModerationClient) Call(ctx context.Context, cfg setting.ContentModerationSetting, text string, images []string) (*ContentModerationAPIResult, error) {
	if c == nil {
		return nil, errors.New("content moderation client not initialized")
	}
	if len(cfg.APIKeys) == 0 {
		return nil, ErrCMNoConfiguredKey
	}

	attempts := cfg.RetryCount + 1
	if attempts <= 0 {
		attempts = 1
	}
	if attempts > setting.MaxContentModerationRetryCount+1 {
		attempts = setting.MaxContentModerationRetryCount + 1
	}

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		key, masked, ok := c.nextUsableKey(cfg)
		if !ok {
			lastErr = ErrCMAllKeysFrozen
			break
		}
		started := time.Now()
		status := 0
		scores, err := c.callOnce(ctx, cfg, key, text, images, &status)
		latency := int(time.Since(started).Milliseconds())
		if err == nil {
			c.markSuccess(key, latency, status)
			result := buildContentModerationResult(scores, cfg.Thresholds)
			result.APIKeyMasked = masked
			result.LatencyMS = latency
			result.HTTPStatus = status
			return result, nil
		}
		c.markFailure(key, err.Error(), latency, status)
		lastErr = err
		// 4xx 多半是请求格式问题，重试也无意义
		if status >= 400 && status < 500 && status != http.StatusTooManyRequests {
			break
		}
		if attempt == attempts-1 {
			break
		}
		wait := time.Duration(200*(attempt+1)) * time.Millisecond
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	if lastErr == nil {
		lastErr = errors.New("content moderation: unknown failure")
	}
	return nil, lastErr
}

func (c *contentModerationClient) callOnce(ctx context.Context, cfg setting.ContentModerationSetting, apiKey, text string, images []string, status *int) (map[string]float64, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = setting.DefaultContentModerationBaseURL
	}
	endpoint, err := url.JoinPath(base, "/v1/moderations")
	if err != nil {
		return nil, err
	}
	payload := buildModerationPayload(cfg, text, images)
	raw, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	timeoutMS := cfg.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = setting.DefaultContentModerationTimeoutMS
	}
	if timeoutMS > setting.MaxContentModerationTimeoutMS {
		timeoutMS = setting.MaxContentModerationTimeoutMS
	}
	reqCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if status != nil {
		*status = resp.StatusCode
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%w: status=%d body=%s", errModerationBadStatus, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out moderationAPIResponse
	if err := common.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("content moderation: decode response: %w", err)
	}
	if len(out.Results) == 0 {
		return nil, ErrCMEmptyResponse
	}
	if out.Results[0].CategoryScores == nil {
		out.Results[0].CategoryScores = map[string]float64{}
	}
	return out.Results[0].CategoryScores, nil
}

func buildModerationPayload(cfg setting.ContentModerationSetting, text string, images []string) any {
	if len(images) == 0 {
		return moderationAPIRequest{
			Model: cfg.Model,
			Input: text,
		}
	}
	parts := make([]moderationAPIInputPart, 0, len(images)+1)
	if strings.TrimSpace(text) != "" {
		parts = append(parts, moderationAPIInputPart{Type: "text", Text: text})
	}
	for _, img := range images {
		parts = append(parts, moderationAPIInputPart{Type: "image_url", ImageURL: &moderationAPIImageURLRef{URL: img}})
	}
	return moderationAPIRequest{
		Model: cfg.Model,
		Input: parts,
	}
}

func buildContentModerationResult(scores map[string]float64, thresholds map[string]float64) *ContentModerationAPIResult {
	flagged := false
	highestCategory := ""
	highestScore := 0.0
	categories := setting.ContentModerationCategories()
	defaults := setting.ContentModerationDefaultThresholds()

	getThreshold := func(cat string) float64 {
		if v, ok := thresholds[cat]; ok {
			return v
		}
		if v, ok := defaults[cat]; ok {
			return v
		}
		return 0.5
	}

	for _, cat := range categories {
		s := scores[cat]
		if s > highestScore || highestCategory == "" {
			highestScore = s
			highestCategory = cat
		}
		if s >= getThreshold(cat) {
			flagged = true
		}
	}
	// 兼容 OpenAI 未来新增的类别
	for cat, s := range scores {
		if s > highestScore {
			highestScore = s
			highestCategory = cat
		}
	}
	return &ContentModerationAPIResult{
		CategoryScores:  scores,
		Flagged:         flagged,
		HighestCategory: highestCategory,
		HighestScore:    highestScore,
	}
}

// nextUsableKey 用 round-robin 选择下一个未冻结的 Key。
func (c *contentModerationClient) nextUsableKey(cfg setting.ContentModerationSetting) (string, string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	n := len(cfg.APIKeys)
	for i := 0; i < n; i++ {
		idx := (c.roundRobin + i) % n
		key := cfg.APIKeys[idx]
		state := c.getOrInitStateLocked(key)
		if state.frozenUntil.IsZero() || !now.Before(state.frozenUntil) {
			state.frozenUntil = time.Time{}
			c.roundRobin = (idx + 1) % n
			return key, state.masked, true
		}
	}
	return "", "", false
}

func (c *contentModerationClient) markSuccess(key string, latencyMS, status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.getOrInitStateLocked(key)
	state.failureCount = 0
	state.successCount++
	state.lastError = ""
	state.lastLatencyMS = latencyMS
	state.lastHTTPStatus = status
	now := time.Now()
	state.lastTestedAt = &now
}

func (c *contentModerationClient) markFailure(key, errMsg string, latencyMS, status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.getOrInitStateLocked(key)
	state.failureCount++
	state.lastError = errMsg
	state.lastLatencyMS = latencyMS
	state.lastHTTPStatus = status
	now := time.Now()
	state.lastTestedAt = &now
	if state.failureCount >= 3 {
		state.frozenUntil = now.Add(60 * time.Second)
	}
}

func (c *contentModerationClient) getOrInitStateLocked(key string) *contentModerationKeyState {
	state, ok := c.keyStates[key]
	if !ok {
		state = &contentModerationKeyState{
			key:    key,
			masked: MaskContentModerationKey(key),
		}
		c.keyStates[key] = state
	}
	return state
}

// Inspect 返回当前配置 Keys 的运行时状态快照（已脱敏，可直接给前端）。
func (c *contentModerationClient) Inspect(cfg setting.ContentModerationSetting) []ContentModerationKeyStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	out := make([]ContentModerationKeyStatus, 0, len(cfg.APIKeys))
	for i, key := range cfg.APIKeys {
		state := c.getOrInitStateLocked(key)
		status := ContentModerationKeyStatus{
			Index:          i,
			Masked:         state.masked,
			Healthy:        state.frozenUntil.IsZero() || !now.Before(state.frozenUntil),
			FailureCount:   state.failureCount,
			SuccessCount:   state.successCount,
			LastError:      state.lastError,
			LastLatencyMS:  state.lastLatencyMS,
			LastHTTPStatus: state.lastHTTPStatus,
		}
		if !state.frozenUntil.IsZero() && now.Before(state.frozenUntil) {
			t := state.frozenUntil
			status.FrozenUntil = &t
		}
		if state.lastTestedAt != nil {
			t := *state.lastTestedAt
			status.LastTestedAt = &t
		}
		out = append(out, status)
	}
	return out
}

// MaskContentModerationKey 把 API Key 脱敏为 "前4***后4"。
func MaskContentModerationKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + "***" + key[len(key)-4:]
}

// --- DTO types (private to this package) ---

type moderationAPIRequest struct {
	Model string `json:"model"`
	Input any    `json:"input"`
}

type moderationAPIInputPart struct {
	Type     string                    `json:"type"`
	Text     string                    `json:"text,omitempty"`
	ImageURL *moderationAPIImageURLRef `json:"image_url,omitempty"`
}

type moderationAPIImageURLRef struct {
	URL string `json:"url"`
}

type moderationAPIResponse struct {
	Results []moderationAPISingleResult `json:"results"`
}

type moderationAPISingleResult struct {
	Flagged        bool               `json:"flagged"`
	CategoryScores map[string]float64 `json:"category_scores"`
}
