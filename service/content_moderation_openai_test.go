package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

func makeCMConfigForTest(serverURL string, keys []string) setting.ContentModerationSetting {
	cfg := setting.GetContentModerationSetting()
	cfg.BaseURL = serverURL
	cfg.Model = "omni-moderation-latest"
	cfg.APIKeys = keys
	cfg.TimeoutMS = 2000
	cfg.RetryCount = 1
	return cfg
}

func TestCMClient_HappyPath_NoFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/moderations", r.URL.Path)
		require.Equal(t, "Bearer sk-test-aaaa-bbbb-cccc-dddd", r.Header.Get("Authorization"))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{"hate":0.01,"sexual":0.02}}]}`))
	}))
	defer srv.Close()

	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-test-aaaa-bbbb-cccc-dddd"})
	res, err := c.Call(context.Background(), cfg, "hello", nil)
	require.NoError(t, err)
	require.False(t, res.Flagged)
	require.Equal(t, "sk-t***dddd", res.APIKeyMasked)
	require.NotEmpty(t, res.HighestCategory)
}

func TestCMClient_HappyPath_Flagged(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":true,"category_scores":{"hate":0.99,"sexual":0.02}}]}`))
	}))
	defer srv.Close()

	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-aaaaaaaaaaaaaaaa"})
	res, err := c.Call(context.Background(), cfg, "bad", nil)
	require.NoError(t, err)
	require.True(t, res.Flagged)
	require.Equal(t, "hate", res.HighestCategory)
	require.InDelta(t, 0.99, res.HighestScore, 1e-6)
}

func TestCMClient_5xxRetryThenSuccess(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("upstream broken"))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer srv.Close()

	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-abcdefghijklmnop"})
	res, err := c.Call(context.Background(), cfg, "hi", nil)
	require.NoError(t, err)
	require.False(t, res.Flagged)
	require.GreaterOrEqual(t, int(calls), 2)
}

func TestCMClient_429SwitchesKey(t *testing.T) {
	var calls int32
	keysUsed := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		auth := r.Header.Get("Authorization")
		keysUsed[auth]++
		if keysUsed[auth] == 1 && auth == "Bearer sk-firstkeyaaaaaaaaa" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer srv.Close()

	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-firstkeyaaaaaaaaa", "sk-secondkeybbbbbbbb"})
	res, err := c.Call(context.Background(), cfg, "x", nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.GreaterOrEqual(t, int(calls), 2)
	require.Equal(t, 1, keysUsed["Bearer sk-firstkeyaaaaaaaaa"])
	require.Equal(t, 1, keysUsed["Bearer sk-secondkeybbbbbbbb"])
}

func TestCMClient_KeyFrozenAfter3Failures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-onlyonekeyxxxxxx"})
	cfg.RetryCount = 0

	for i := 0; i < 3; i++ {
		_, err := c.Call(context.Background(), cfg, "x", nil)
		require.Error(t, err)
	}
	statuses := c.Inspect(cfg)
	require.Len(t, statuses, 1)
	require.False(t, statuses[0].Healthy)
	require.NotNil(t, statuses[0].FrozenUntil)
	require.Equal(t, 3, statuses[0].FailureCount)

	// 第四次：应该立即返回 ErrCMAllKeysFrozen，因为唯一的 key 已冻结
	_, err := c.Call(context.Background(), cfg, "x", nil)
	require.True(t, errors.Is(err, ErrCMAllKeysFrozen), "got err=%v", err)
}

func TestCMClient_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-key12345678abcd"})
	cfg.TimeoutMS = 50
	cfg.RetryCount = 0
	_, err := c.Call(context.Background(), cfg, "x", nil)
	require.Error(t, err)
}

func TestCMClient_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`not-json-at-all`))
	}))
	defer srv.Close()
	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-key12345678abcd"})
	cfg.RetryCount = 0
	_, err := c.Call(context.Background(), cfg, "x", nil)
	require.Error(t, err)
}

func TestCMClient_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer srv.Close()
	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-key12345678abcd"})
	cfg.RetryCount = 0
	_, err := c.Call(context.Background(), cfg, "x", nil)
	require.ErrorIs(t, err, ErrCMEmptyResponse)
}

func TestCMClient_NoKeyConfigured(t *testing.T) {
	c := newContentModerationClient(nil)
	cfg := setting.GetContentModerationSetting()
	cfg.APIKeys = []string{}
	_, err := c.Call(context.Background(), cfg, "x", nil)
	require.ErrorIs(t, err, ErrCMNoConfiguredKey)
}

func TestCMClient_ImagePayload(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		receivedBody = body[:n]
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"results":[{"flagged":false,"category_scores":{}}]}`))
	}))
	defer srv.Close()
	c := newContentModerationClient(srv.Client())
	cfg := makeCMConfigForTest(srv.URL, []string{"sk-key12345678abcd"})
	_, err := c.Call(context.Background(), cfg, "hello", []string{"https://example.com/x.png"})
	require.NoError(t, err)
	require.Contains(t, string(receivedBody), `"type":"image_url"`)
	require.Contains(t, string(receivedBody), "https://example.com/x.png")
}

func TestMaskContentModerationKey(t *testing.T) {
	require.Equal(t, "abcd***wxyz", MaskContentModerationKey("abcdefghijklmnopqrstuvwxyz"))
	require.Equal(t, "****", MaskContentModerationKey("abcd"))
	require.Equal(t, "", MaskContentModerationKey(""))
	require.Equal(t, "1234***wxyz", MaskContentModerationKey("  1234abcdefwxyz  "))
}

func TestBuildContentModerationResult_ThresholdEval(t *testing.T) {
	scores := map[string]float64{
		"hate":     0.7,
		"sexual":   0.5,
		"violence": 0.8,
	}
	thresholds := map[string]float64{
		"hate":     0.65,
		"sexual":   0.9,
		"violence": 0.9,
	}
	r := buildContentModerationResult(scores, thresholds)
	require.True(t, r.Flagged)
	require.Equal(t, "violence", r.HighestCategory)
	require.InDelta(t, 0.8, r.HighestScore, 1e-6)
}

func TestBuildContentModerationResult_NoneFlagged(t *testing.T) {
	scores := map[string]float64{"hate": 0.1}
	thresholds := setting.ContentModerationDefaultThresholds()
	r := buildContentModerationResult(scores, thresholds)
	require.False(t, r.Flagged)
}
