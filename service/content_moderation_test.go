package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

// memoryLogSink 是测试用 Log Sink。
type memoryLogSink struct {
	mu      sync.Mutex
	records []ContentModerationLogRecord
}

func (m *memoryLogSink) Write(_ context.Context, rec ContentModerationLogRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.records = append(m.records, rec)
}

func (m *memoryLogSink) All() []ContentModerationLogRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ContentModerationLogRecord, len(m.records))
	copy(out, m.records)
	return out
}

func newTestCMServiceForCheck(t *testing.T, srvURL string) (*ContentModerationService, *memoryLogSink) {
	t.Helper()
	sink := &memoryLogSink{}
	enqueued := func(_ ContentModerationCheckRequest) {}
	s := &ContentModerationService{
		logSink:        sink,
		enqueueObserve: enqueued,
	}
	// 替换全局 CMClient 为测试用 http.Client
	prev := CMClient
	CMClient = newContentModerationClient(http.DefaultClient)
	t.Cleanup(func() { CMClient = prev })
	// 替换 HashCache 为内存实现
	prevCache := CMHashCache
	CMHashCache = NewMemoryContentModerationHashCache()
	t.Cleanup(func() { CMHashCache = prevCache })

	configureCMForTest(srvURL)
	return s, sink
}

func configureCMForTest(srvURL string) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	s.BaseURL = srvURL
	s.Model = "omni-moderation-latest"
	s.APIKeys = []string{"sk-test1234567890ab"}
	s.TimeoutMS = 2000
	s.RetryCount = 0
	s.SampleRate = 100
	s.InputScope = setting.ContentModerationInputScopeLastUser
	s.PreHashCheckEnabled = true
	s.ModelMode = setting.ContentModerationModelModeAll
	s.RecordNonHits = true
	setting.UnlockContentModeration()
}

func TestCMService_Disabled_ReturnsAllow(t *testing.T) {
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Enabled = false
	setting.UnlockContentModeration()
	t.Cleanup(func() {
		setting.LockContentModeration()
		setting.MutableContentModerationSetting().Enabled = true
		setting.UnlockContentModeration()
	})

	s := &ContentModerationService{logSink: &memoryLogSink{}}
	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
}

func TestCMService_ModelOutOfScope_Skipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called when model is out of scope")
	}))
	defer srv.Close()
	s, sink := newTestCMServiceForCheck(t, srv.URL)

	setting.LockContentModeration()
	c := setting.MutableContentModerationSetting()
	c.ModelMode = setting.ContentModerationModelModeWhitelist
	c.ModelList = []string{"gpt-4*"}
	setting.UnlockContentModeration()

	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "claude-sonnet",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
	require.Empty(t, sink.All())
}

func TestCMService_PreBlock_FlaggedHits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":true,"category_scores":{"hate":0.99}}]}`))
	}))
	defer srv.Close()
	s, sink := newTestCMServiceForCheck(t, srv.URL)

	var autoBanCalls int
	s.autoBanCallback = func(_ context.Context, userID int, rec *ContentModerationLogRecord) {
		autoBanCalls++
		require.Equal(t, 42, userID)
	}

	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   42,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hate this"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionBlock, res.Action)
	require.True(t, res.Flagged)
	require.Equal(t, "hate", res.HighestCategory)
	require.Equal(t, 1, autoBanCalls)
	require.Len(t, sink.All(), 1)
	require.Equal(t, setting.ContentModerationLayerOpenAI, sink.All()[0].DetectionLayer)
}

func TestCMService_HashPreCheckShortCircuits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called when hash pre-check hits")
	}))
	defer srv.Close()
	s, sink := newTestCMServiceForCheck(t, srv.URL)

	// 先计算输入 hash 并塞进 cache
	body := []byte(`{"messages":[{"role":"user","content":"banned topic"}]}`)
	input := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, body, setting.ContentModerationInputScopeLastUser)
	require.NoError(t, CMHashCache.Record(context.Background(), input.Hash))

	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     body,
	})
	require.Equal(t, setting.ContentModerationActionHashBlock, res.Action)
	require.True(t, res.Flagged)
	require.Len(t, sink.All(), 1)
}

func TestCMService_Observe_EnqueuesAndAllows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("observe should not call upstream synchronously")
	}))
	defer srv.Close()
	s, _ := newTestCMServiceForCheck(t, srv.URL)
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Mode = setting.ContentModerationModeObserve
	setting.UnlockContentModeration()

	var enqueued int
	s.enqueueObserve = func(_ ContentModerationCheckRequest) {
		enqueued++
	}
	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
	require.Equal(t, 1, enqueued)
}

func TestCMService_AllKeysFrozen_FailsOpen(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()
	s, sink := newTestCMServiceForCheck(t, srv.URL)
	// 把 key 调到只有一把，连续失败后会冻结
	for i := 0; i < 3; i++ {
		_ = s.Check(context.Background(), ContentModerationCheckRequest{
			UserID:   1,
			Model:    "gpt-4o",
			Protocol: ContentModerationProtocolOpenAIChat,
			Body:     []byte(`{"messages":[{"role":"user","content":"hi-` + string(rune('a'+i)) + `"}]}`),
		})
	}
	res := s.Check(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi-last"}]}`),
	})
	// fail-open: 主链路放行 + error 日志
	require.Equal(t, setting.ContentModerationActionAllow, res.Action)
	// 至少 4 条 error 日志
	require.GreaterOrEqual(t, len(sink.All()), 4)
}

func TestRedactPII(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"a@b.com", "<PII>"},
		{"contact alice@example.org for help", "contact <PII> for help"},
		{"call 13812345678 now", "call <PII> now"},
		{"my id 110101199003073872 ok", "my id <PII> ok"},
		{"safe text without secrets", "safe text without secrets"},
	}
	for _, c := range cases {
		require.Equal(t, c.want, redactContentModerationPII(c.in), c.in)
	}
}

func TestHashedMod_Stable(t *testing.T) {
	h := "abcdef1234567890" + "0000000000000000" + "0000000000000000" + "0000000000000000"
	a := hashedMod(h, 100)
	b := hashedMod(h, 100)
	require.Equal(t, a, b)
	require.LessOrEqual(t, a, 99)
	require.GreaterOrEqual(t, a, 0)
}

func TestBuildContentModerationExcerpt_Truncates(t *testing.T) {
	in := "alice@example.com " + repeat("X", 1000)
	out := buildContentModerationExcerpt(in)
	require.Contains(t, out, "<PII>")
	require.LessOrEqual(t, len([]rune(out)), setting.MaxContentModerationExcerptRunes)
}

func TestInitContentModerationService_InjectsDeps(t *testing.T) {
	sink := &memoryLogSink{}
	enqueueCalled := false
	autoBanCalled := false
	InitContentModerationService(
		sink,
		func(_ ContentModerationCheckRequest) { enqueueCalled = true },
		func(_ context.Context, _ int, _ *ContentModerationLogRecord) { autoBanCalled = true },
	)
	t.Cleanup(func() { CMService = &ContentModerationService{} })
	require.NotNil(t, CMService.logSink)
	require.NotNil(t, CMService.enqueueObserve)
	require.NotNil(t, CMService.autoBanCallback)
	CMService.enqueueObserve(ContentModerationCheckRequest{})
	CMService.autoBanCallback(context.Background(), 1, nil)
	require.True(t, enqueueCalled)
	require.True(t, autoBanCalled)
}

func TestCheckObserveAsync_WritesLogOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":true,"category_scores":{"hate":0.99}}]}`))
	}))
	defer srv.Close()
	s, sink := newTestCMServiceForCheck(t, srv.URL)
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Mode = setting.ContentModerationModeObserve
	setting.UnlockContentModeration()

	s.CheckObserveAsync(context.Background(), ContentModerationCheckRequest{
		UserID:   1,
		Model:    "gpt-4o",
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"bad"}]}`),
	})
	require.Len(t, sink.All(), 1)
	require.Equal(t, setting.ContentModerationActionAllow, sink.All()[0].Action,
		"observe mode never aborts")
}

func TestHexNibble_AllPaths(t *testing.T) {
	require.Equal(t, 0, hexNibble('0'))
	require.Equal(t, 9, hexNibble('9'))
	require.Equal(t, 10, hexNibble('a'))
	require.Equal(t, 15, hexNibble('f'))
	require.Equal(t, 10, hexNibble('A'))
	require.Equal(t, 15, hexNibble('F'))
	require.Equal(t, 0, hexNibble('z')) // default
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
