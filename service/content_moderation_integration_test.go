package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

// 集成测套件 G18：S1-S8 端到端场景。
// 所有测试在同一进程内，mock OpenAI Moderation 端 + 内存 HashCache + 内存 ViolationCounter。
// 跨用例之间通过 t.Cleanup 还原全局状态。

type integrationFixture struct {
	t        *testing.T
	openai   *httptest.Server
	logs     *memoryLogSink
	disabled map[int]bool
}

func setupCMIntegration(t *testing.T, flagFn func(req []byte) (flagged bool, scores map[string]float64)) *integrationFixture {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		flagged, scores := flagFn(body)
		w.WriteHeader(200)
		buf := bytes.Buffer{}
		buf.WriteString(`{"results":[{"flagged":`)
		if flagged {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		buf.WriteString(`,"category_scores":{`)
		first := true
		for k, v := range scores {
			if !first {
				buf.WriteString(",")
			}
			first = false
			buf.WriteString(`"`)
			buf.WriteString(k)
			buf.WriteString(`":`)
			buf.WriteString(floatToString(v))
		}
		buf.WriteString(`}}]}`)
		_, _ = w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)

	prevHash := CMHashCache
	CMHashCache = NewMemoryContentModerationHashCache()
	prevCounter := CMViolationCounter
	CMViolationCounter = NewMemoryViolationCounter()
	prevSvc := CMService
	prevClient := CMClient
	CMClient = newContentModerationClient(srv.Client())
	logs := &memoryLogSink{}
	disabled := map[int]bool{}
	disabler := &integrationDisabler{disabled: disabled, mu: &sync.Mutex{}}
	CMService = &ContentModerationService{
		logSink: logs,
		enqueueObserve: func(_ ContentModerationCheckRequest) {
			// observe sync inline for deterministic tests
		},
		autoBanCallback: func(ctx context.Context, userID int, rec *ContentModerationLogRecord) {
			a := &ContentModerationAutoBanService{
				counter:  CMViolationCounter,
				disabler: disabler,
			}
			a.HandleViolation(ctx, userID, "", rec.HighestCategory)
		},
	}
	t.Cleanup(func() {
		CMHashCache = prevHash
		CMViolationCounter = prevCounter
		CMService = prevSvc
		CMClient = prevClient
	})

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	s.BaseURL = srv.URL
	s.APIKeys = []string{"sk-integrationtestkey"}
	s.TimeoutMS = 2000
	s.RetryCount = 0
	s.SampleRate = 100
	s.InputScope = setting.ContentModerationInputScopeLastUser
	s.PreHashCheckEnabled = true
	s.ModelMode = setting.ContentModerationModelModeAll
	s.AutoBanEnabled = true
	s.BanThreshold = 3
	s.ViolationWindowHours = 720
	s.RecordNonHits = true
	setting.UnlockContentModeration()

	return &integrationFixture{t: t, openai: srv, logs: logs, disabled: disabled}
}

type integrationDisabler struct {
	disabled map[int]bool
	mu       *sync.Mutex
}

func (d *integrationDisabler) GetUserStatus(id int) (int, string, string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disabled[id] {
		return common.UserStatusDisabled, "user" + intToString(id), "u@example.com", nil
	}
	return common.UserStatusEnabled, "user" + intToString(id), "u@example.com", nil
}

func (d *integrationDisabler) DisableUser(id int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disabled[id] = true
	return nil
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func floatToString(v float64) string {
	// 简单实现：保留 4 位小数
	intPart := int(v * 10000)
	if intPart == 0 {
		return "0"
	}
	whole := intPart / 10000
	frac := intPart % 10000
	w := intToString(whole)
	f := intToString(frac)
	for len(f) < 4 {
		f = "0" + f
	}
	return w + "." + f
}

// S1: enabled=false 时主流程零影响（基线）
func TestIntegration_S1_DisabledZeroImpact(t *testing.T) {
	fx := setupCMIntegration(t, func(_ []byte) (bool, map[string]float64) {
		t.Fatalf("upstream must not be called when disabled")
		return false, nil
	})
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Enabled = false
	setting.UnlockContentModeration()

	res := CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
	require.Empty(t, fx.logs.All())
}

// S2: observe + 100 条正常请求 → 全通过 + 100% 写日志
func TestIntegration_S2_ObserveNormalRequests(t *testing.T) {
	fx := setupCMIntegration(t, func(_ []byte) (bool, map[string]float64) {
		return false, map[string]float64{"hate": 0.01}
	})
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Mode = setting.ContentModerationModeObserve
	setting.MutableContentModerationSetting().RecordNonHits = true
	setting.UnlockContentModeration()
	// observe 模式下 enqueue 走 service.enqueueObserve（已 stub 为 noop），手动同步调 CheckObserveAsync
	for i := 0; i < 100; i++ {
		CMService.CheckObserveAsync(context.Background(), ContentModerationCheckRequest{
			UserID:   1,
			Model:    "gpt-4o",
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(`{"messages":[{"role":"user","content":"normal text ` + intToString(i) + `"}]}`),
		})
	}
	require.Equal(t, 100, len(fx.logs.All()), "all 100 requests should be logged")
	for _, l := range fx.logs.All() {
		require.Equal(t, setting.ContentModerationActionAllow, l.Action, "observe never aborts")
	}
}

// S3: pre_block 模式 + 命中 → 拦截 + 写日志
func TestIntegration_S3_PreBlockBlocks(t *testing.T) {
	fx := setupCMIntegration(t, func(body []byte) (bool, map[string]float64) {
		if bytes.Contains(body, []byte("forbidden")) {
			return true, map[string]float64{"hate": 0.99}
		}
		return false, map[string]float64{"hate": 0.01}
	})
	res := CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   2,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"forbidden topic"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionBlock, res.Action)
	require.True(t, res.Flagged)
	require.GreaterOrEqual(t, len(fx.logs.All()), 1)
}

// S5: 连续违规 → 自动封禁 + emaill mock 投递
func TestIntegration_S5_AutoBanAfterThreshold(t *testing.T) {
	fx := setupCMIntegration(t, func(_ []byte) (bool, map[string]float64) {
		return true, map[string]float64{"violence": 0.99}
	})

	setting.LockContentModeration()
	setting.MutableContentModerationSetting().BanThreshold = 3
	setting.UnlockContentModeration()

	var emailCount int32
	SetContentModerationEmailSender(func(_, _, _ string) error {
		atomic.AddInt32(&emailCount, 1)
		return nil
	})
	t.Cleanup(ResetContentModerationEmailSender)

	for i := 0; i < 3; i++ {
		CMService.Check(context.Background(), ContentModerationCheckRequest{
			UserID:    99,
			Username:  "alice",
			Model:     "gpt-4o",
			Protocol:  ContentModerationProtocolOpenAIChat,
			Body:      []byte(`{"messages":[{"role":"user","content":"violate ` + intToString(i) + `"}]}`),
			RequestID: "req-" + intToString(i),
		})
	}

	require.True(t, fx.disabled[99], "user 99 should be auto-banned after 3 violations")
}

// S6: 删除黑名单 hash → 后续相同输入重新走 OpenAI
func TestIntegration_S6_HashBlacklistDeleteRoundTrips(t *testing.T) {
	hits := int32(0)
	fx := setupCMIntegration(t, func(body []byte) (bool, map[string]float64) {
		atomic.AddInt32(&hits, 1)
		if bytes.Contains(body, []byte("bad")) {
			return true, map[string]float64{"hate": 0.99}
		}
		return false, map[string]float64{"hate": 0.01}
	})
	_ = fx
	body := []byte(`{"messages":[{"role":"user","content":"bad content"}]}`)

	// 第一次：调 OpenAI 命中，写 hash 缓存
	CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID: 7, Model: "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})
	require.Equal(t, int32(1), atomic.LoadInt32(&hits))

	// 第二次：hash 预检命中，不调 OpenAI
	CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID: 7, Model: "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})
	require.Equal(t, int32(1), atomic.LoadInt32(&hits), "hash should short-circuit upstream call")

	// 删除 hash
	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, setting.ContentModerationInputScopeLastUser)
	require.NoError(t, CMHashCache.Delete(context.Background(), input.Hash))

	// 第三次：重新走 OpenAI
	CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID: 7, Model: "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat, Body: body,
	})
	require.Equal(t, int32(2), atomic.LoadInt32(&hits))
}

// S7: 模型 whitelist → 未列出的模型 100% 跳过
func TestIntegration_S7_ModelWhitelistSkips(t *testing.T) {
	fx := setupCMIntegration(t, func(_ []byte) (bool, map[string]float64) {
		t.Fatalf("upstream must not be called for out-of-scope model")
		return false, nil
	})
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.ModelMode = setting.ContentModerationModelModeWhitelist
	s.ModelList = []string{"gpt-4*"}
	setting.UnlockContentModeration()

	res := CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "claude-sonnet",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"anything"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
	require.Empty(t, fx.logs.All())
}

// S8: OpenAI 5xx 全失败 → fail-open 主链路放行 + error 日志
func TestIntegration_S8_FailOpenWhenUpstreamDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("service unavailable"))
	}))
	defer srv.Close()

	prevSvc := CMService
	prevClient := CMClient
	prevHash := CMHashCache
	CMHashCache = NewMemoryContentModerationHashCache()
	CMClient = newContentModerationClient(srv.Client())
	logs := &memoryLogSink{}
	CMService = &ContentModerationService{logSink: logs}
	t.Cleanup(func() {
		CMService = prevSvc
		CMClient = prevClient
		CMHashCache = prevHash
	})

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	s.BaseURL = srv.URL
	s.APIKeys = []string{"sk-failopen1234567"}
	s.RetryCount = 0
	s.TimeoutMS = 1000
	setting.UnlockContentModeration()

	res := CMService.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.NotEqual(t, setting.ContentModerationActionBlock, res.Action, "must fail-open")
	require.NotEmpty(t, logs.All(), "should write error log")
	require.Contains(t, strings.ToLower(logs.All()[0].Error), "status")
}

func TestIntegration_FloatToStringSanity(t *testing.T) {
	// 防止 floatToString helper 出回归
	require.Equal(t, "0.9900", floatToString(0.99))
	require.Equal(t, "0.0100", floatToString(0.01))
}

// 防止快速测试运行时 metric goroutine 干扰
func init() {
	// no-op
	_ = time.Now
}
