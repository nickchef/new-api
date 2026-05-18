package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func buildCMAdminTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/admin/content_moderation")
	{
		g.GET("/config", GetContentModerationConfig)
		g.GET("/status", GetContentModerationStatus)
		g.POST("/preview", PreviewContentModeration)
		g.GET("/flagged_hash/count", GetContentModerationFlaggedHashCount)
		g.POST("/flagged_hash/clear", ClearContentModerationFlaggedHashes)
		g.DELETE("/flagged_hash", DeleteContentModerationFlaggedHash)
		g.GET("/violation_count/:user_id", GetContentModerationViolationCount)
	}
	return r
}

func TestCMAdmin_GetConfig_MasksKeys(t *testing.T) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.APIKeys = []string{"sk-abcdefghijklmnop"}
	setting.UnlockContentModeration()

	r := buildCMAdminTestRouter()
	req := httptest.NewRequest("GET", "/api/admin/content_moderation/config", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "api_key_masks")
	require.Contains(t, body, "sk-a***mnop")
	require.NotContains(t, body, "ijklmnop\"") // 完整 key 不应泄露
}

func TestCMAdmin_GetStatus_Snapshot(t *testing.T) {
	r := buildCMAdminTestRouter()
	req := httptest.NewRequest("GET", "/api/admin/content_moderation/status", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	require.Contains(t, body, "worker")
	require.Contains(t, body, "api_keys")
}

func TestCMAdmin_FlaggedHashCount_Empty(t *testing.T) {
	service.CMHashCache = service.NewMemoryContentModerationHashCache()
	r := buildCMAdminTestRouter()
	req := httptest.NewRequest("GET", "/api/admin/content_moderation/flagged_hash/count", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"count":0`)
}

func TestCMAdmin_FlaggedHashDeleteAndClear(t *testing.T) {
	service.CMHashCache = service.NewMemoryContentModerationHashCache()
	_ = service.CMHashCache.Record(nil, "h1")
	_ = service.CMHashCache.Record(nil, "h2")

	r := buildCMAdminTestRouter()
	// DELETE
	req := httptest.NewRequest("DELETE", "/api/admin/content_moderation/flagged_hash",
		strings.NewReader(`{"hash":"h1"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// COUNT 应剩 1
	req = httptest.NewRequest("GET", "/api/admin/content_moderation/flagged_hash/count", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Contains(t, rec.Body.String(), `"count":1`)

	// CLEAR
	req = httptest.NewRequest("POST", "/api/admin/content_moderation/flagged_hash/clear", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// 应清零
	req = httptest.NewRequest("GET", "/api/admin/content_moderation/flagged_hash/count", nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Contains(t, rec.Body.String(), `"count":0`)
}

func TestCMAdmin_ViolationCount(t *testing.T) {
	service.CMViolationCounter = service.NewMemoryViolationCounter()
	r := buildCMAdminTestRouter()
	req := httptest.NewRequest("GET", "/api/admin/content_moderation/violation_count/42", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"user_id":42`)
}

func TestCMAdmin_PreviewWithoutKey_ReturnsError(t *testing.T) {
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().APIKeys = []string{}
	setting.UnlockContentModeration()
	service.InitContentModerationClient(nil)

	r := buildCMAdminTestRouter()
	req := httptest.NewRequest("POST", "/api/admin/content_moderation/preview",
		strings.NewReader(`{"text":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	// 返回 200 + success=false（无 key 配置）
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"success":false`)
}

func TestNormalizeContentModerationKeys_DedupAndTrim(t *testing.T) {
	out := normalizeContentModerationKeys([]string{"  key1  ", "key1", "", "key2"})
	require.Equal(t, []string{"key1", "key2"}, out)
}

func TestClampInt(t *testing.T) {
	require.Equal(t, 5, clampInt(0, 5, 10))
	require.Equal(t, 10, clampInt(20, 5, 10))
	require.Equal(t, 7, clampInt(7, 5, 10))
}

func TestMergeContentModerationThresholds_ClampsRange(t *testing.T) {
	base := map[string]float64{"hate": 0.5}
	override := map[string]float64{"hate": 1.5, "sexual": -0.2}
	merged := mergeContentModerationThresholds(base, override)
	require.Equal(t, 1.0, merged["hate"])
	require.Equal(t, 0.0, merged["sexual"])
}
