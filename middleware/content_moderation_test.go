package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func buildCMTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ContentModeration())
	r.POST("/v1/chat/completions", func(c *gin.Context) {
		// 读取一次 body 模拟 controller 行为
		b, _ := io.ReadAll(c.Request.Body)
		c.String(200, string(b))
	})
	r.POST("/v1/models", func(c *gin.Context) {
		c.String(200, "models-ok")
	})
	r.GET("/v1/models", func(c *gin.Context) {
		c.String(200, "models-list")
	})
	return r
}

func resetCMSetting() {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = false
	s.Mode = setting.ContentModerationModeOff
	setting.UnlockContentModeration()
}

func TestMiddleware_Disabled_Passes(t *testing.T) {
	resetCMSetting()
	r := buildCMTestRouter()
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, body, rec.Body.String(), "body should be readable by controller")
}

func TestMiddleware_SkipsMetadataPaths(t *testing.T) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	setting.UnlockContentModeration()
	t.Cleanup(resetCMSetting)

	r := buildCMTestRouter()
	for _, path := range []string{"/v1/models", "/v1/moderations"} {
		req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		require.NotEqual(t, http.StatusForbidden, rec.Code, "should not block %s", path)
	}
}

func TestMiddleware_L2Hit_BlocksWithOpenAIFormat(t *testing.T) {
	// 注入 CMService 的 fake，强制返回 Block
	prev := service.CMService
	service.CMService = newFakeBlockingCMService()
	t.Cleanup(func() { service.CMService = prev })

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	s.BlockMessage = "policy blocked"
	setting.UnlockContentModeration()
	t.Cleanup(resetCMSetting)

	r := buildCMTestRouter()
	body := `{"messages":[{"role":"user","content":"banned text"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "content_policy_violation")
	require.Contains(t, rec.Body.String(), "policy blocked")
}

func TestMiddleware_AllowPath_LeavesBodyReadable(t *testing.T) {
	// 用 mock OpenAI 返回 flagged=false，验证 allow 路径下 body 仍可被 controller 读取
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer srv.Close()
	service.InitContentModerationClient(srv.Client())
	service.InitContentModerationService(noopSink{}, func(_ service.ContentModerationCheckRequest) {}, nil)

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	s.Mode = setting.ContentModerationModePreBlock
	s.BaseURL = srv.URL
	s.APIKeys = []string{"sk-test1234567890ab"}
	setting.UnlockContentModeration()
	t.Cleanup(resetCMSetting)

	r := buildCMTestRouter()
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
	require.Equal(t, body, rec.Body.String(),
		"body must remain readable after middleware")
}

func TestMiddleware_GETMethodPasses(t *testing.T) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.Enabled = true
	setting.UnlockContentModeration()
	t.Cleanup(resetCMSetting)

	r := buildCMTestRouter()
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code)
}

func TestMiddleware_SkipFunc(t *testing.T) {
	require.True(t, shouldSkipContentModerationPath("/v1/models"))
	require.True(t, shouldSkipContentModerationPath("/v1/moderations"))
	require.True(t, shouldSkipContentModerationPath("/v1/files/abc"))
	require.True(t, shouldSkipContentModerationPath("/health"))
	require.True(t, shouldSkipContentModerationPath("/v1/realtime"))
	require.False(t, shouldSkipContentModerationPath("/v1/chat/completions"))
	require.False(t, shouldSkipContentModerationPath("/v1/messages"))
	// Gemini :generateContent on /v1beta/models/<name>:generateContent SHOULD audit
	require.False(t, shouldSkipContentModerationPath("/v1beta/models/gemini-pro:generateContent"))
}

// --- helpers ---

type fakeBlockingService struct{}

func newFakeBlockingCMService() *service.ContentModerationService {
	// 借助 testing-friendly init
	s := &service.ContentModerationService{}
	// 通过反射不便，借助 setting 设置 Enabled=true + 自定义 check 由于无法替换，
	// 改为：把 CheckPath 用真实路径走 service.Check，但 mock 上游 OpenAI 返回 flagged
	// 这里我们直接调用 Init...Service 注入 sink 但保留默认 enqueue/autoban
	prevHash := service.CMHashCache
	service.CMHashCache = service.NewMemoryContentModerationHashCache()
	_ = prevHash // 不还原，调用方负责

	// 设置 OpenAI 客户端指向一个永远 flagged 的 mock
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":true,"category_scores":{"hate":0.99}}]}`))
	}))
	// 不 close srv —— 让其在测试期间存活；进程退出时 OS 回收。
	// 替换 CMClient 的 base URL 通过 setting
	setting.LockContentModeration()
	cfg := setting.MutableContentModerationSetting()
	cfg.BaseURL = srv.URL
	cfg.APIKeys = []string{"sk-faketestkey1234"}
	cfg.Model = "omni-moderation-latest"
	cfg.TimeoutMS = 2000
	cfg.RetryCount = 0
	setting.UnlockContentModeration()
	service.InitContentModerationService(noopSink{}, func(_ service.ContentModerationCheckRequest) {}, nil)
	// 注入一个 http.Client 给 CMClient
	service.InitContentModerationClient(srv.Client())
	_ = fakeBlockingService{}
	return s
}

type noopSink struct{}

func (noopSink) Write(_ context.Context, _ service.ContentModerationLogRecord) {}
