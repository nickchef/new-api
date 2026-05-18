package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// GET /api/admin/content_moderation/config
func GetContentModerationConfig(c *gin.Context) {
	cfg := setting.GetContentModerationSetting()
	view := contentModerationConfigView(cfg)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": view})
}

// PUT /api/admin/content_moderation/config
func UpdateContentModerationConfig(c *gin.Context) {
	var input struct {
		Enabled              *bool               `json:"enabled"`
		Mode                 *string             `json:"mode"`
		BaseURL              *string             `json:"base_url"`
		Model                *string             `json:"model"`
		APIKeys              *[]string           `json:"api_keys"`
		TimeoutMS            *int                `json:"timeout_ms"`
		RetryCount           *int                `json:"retry_count"`
		Thresholds           *map[string]float64 `json:"thresholds"`
		SampleRate           *int                `json:"sample_rate"`
		InputScope           *string             `json:"input_scope"`
		PreHashCheckEnabled  *bool               `json:"pre_hash_check_enabled"`
		ModelMode            *string             `json:"model_mode"`
		ModelList            *[]string           `json:"model_list"`
		BlockStatus          *int                `json:"block_status"`
		BlockMessage         *string             `json:"block_message"`
		AutoBanEnabled       *bool               `json:"auto_ban_enabled"`
		BanThreshold         *int                `json:"ban_threshold"`
		ViolationWindowHours *int                `json:"violation_window_hours"`
		EmailOnHit           *bool               `json:"email_on_hit"`
		EmailToAdmin         *bool               `json:"email_to_admin"`
		EmailToUser          *bool               `json:"email_to_user"`
		WorkerCount          *int                `json:"worker_count"`
		QueueSize            *int                `json:"queue_size"`
		RecordNonHits        *bool               `json:"record_non_hits"`
		HitRetentionDays     *int                `json:"hit_retention_days"`
		NonHitRetentionDays  *int                `json:"non_hit_retention_days"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	if input.Enabled != nil {
		s.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		s.Mode = *input.Mode
	}
	if input.BaseURL != nil {
		s.BaseURL = strings.TrimSpace(*input.BaseURL)
	}
	if input.Model != nil {
		s.Model = strings.TrimSpace(*input.Model)
	}
	if input.APIKeys != nil {
		s.APIKeys = normalizeContentModerationKeys(*input.APIKeys)
	}
	if input.TimeoutMS != nil {
		s.TimeoutMS = clampInt(*input.TimeoutMS, setting.MinContentModerationTimeoutMS, setting.MaxContentModerationTimeoutMS)
	}
	if input.RetryCount != nil {
		s.RetryCount = clampInt(*input.RetryCount, 0, setting.MaxContentModerationRetryCount)
	}
	if input.Thresholds != nil {
		s.Thresholds = mergeContentModerationThresholds(s.Thresholds, *input.Thresholds)
	}
	if input.SampleRate != nil {
		s.SampleRate = clampInt(*input.SampleRate, 0, 100)
	}
	if input.InputScope != nil {
		s.InputScope = *input.InputScope
	}
	if input.PreHashCheckEnabled != nil {
		s.PreHashCheckEnabled = *input.PreHashCheckEnabled
	}
	if input.ModelMode != nil {
		s.ModelMode = *input.ModelMode
	}
	if input.ModelList != nil {
		s.ModelList = *input.ModelList
	}
	if input.BlockStatus != nil {
		s.BlockStatus = *input.BlockStatus
	}
	if input.BlockMessage != nil {
		s.BlockMessage = *input.BlockMessage
	}
	if input.AutoBanEnabled != nil {
		s.AutoBanEnabled = *input.AutoBanEnabled
	}
	if input.BanThreshold != nil {
		s.BanThreshold = clampInt(*input.BanThreshold, 1, 100000)
	}
	if input.ViolationWindowHours != nil {
		s.ViolationWindowHours = clampInt(*input.ViolationWindowHours, 1, 24*365)
	}
	if input.EmailOnHit != nil {
		s.EmailOnHit = *input.EmailOnHit
	}
	if input.EmailToAdmin != nil {
		s.EmailToAdmin = *input.EmailToAdmin
	}
	if input.EmailToUser != nil {
		s.EmailToUser = *input.EmailToUser
	}
	if input.WorkerCount != nil {
		s.WorkerCount = clampInt(*input.WorkerCount, 0, setting.MaxContentModerationWorkerCount)
	}
	if input.QueueSize != nil {
		s.QueueSize = clampInt(*input.QueueSize, 0, setting.MaxContentModerationQueueSize)
	}
	if input.RecordNonHits != nil {
		s.RecordNonHits = *input.RecordNonHits
	}
	if input.HitRetentionDays != nil {
		s.HitRetentionDays = clampInt(*input.HitRetentionDays, 0, setting.MaxContentModerationRetentionDays)
	}
	if input.NonHitRetentionDays != nil {
		s.NonHitRetentionDays = clampInt(*input.NonHitRetentionDays, 0, setting.MaxContentModerationRetentionDays)
	}
	setting.UnlockContentModeration()

	// 持久化到 option 表
	if err := persistContentModerationSetting(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GET /api/admin/content_moderation/status
func GetContentModerationStatus(c *gin.Context) {
	cfg := setting.GetContentModerationSetting()
	stats := service.InspectContentModerationWorkerStats()
	keys := service.CMClient.Inspect(cfg)
	hashCount, _ := service.CMHashCache.Count(c.Request.Context())
	metrics := service.InspectContentModerationMetrics()
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"enabled":            cfg.Enabled,
			"mode":               cfg.Mode,
			"worker":             stats,
			"api_keys":           keys,
			"flagged_hash_count": hashCount,
			"metrics":            metrics,
		},
	})
}

// POST /api/admin/content_moderation/test_api_keys
func TestContentModerationAPIKeys(c *gin.Context) {
	var input struct {
		APIKeys   []string `json:"api_keys"`
		BaseURL   string   `json:"base_url"`
		Model     string   `json:"model"`
		TimeoutMS int      `json:"timeout_ms"`
		Prompt    string   `json:"prompt"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	cfg := setting.GetContentModerationSetting()
	if input.BaseURL != "" {
		cfg.BaseURL = input.BaseURL
	}
	if input.Model != "" {
		cfg.Model = input.Model
	}
	if input.TimeoutMS > 0 {
		cfg.TimeoutMS = input.TimeoutMS
	}
	if len(input.APIKeys) > 0 {
		cfg.APIKeys = normalizeContentModerationKeys(input.APIKeys)
	}
	prompt := input.Prompt
	if prompt == "" {
		prompt = "test"
	}

	result, err := service.CMClient.Call(c.Request.Context(), cfg, prompt, nil)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error(), "data": gin.H{
			"keys": service.CMClient.Inspect(cfg),
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"audit": gin.H{
				"flagged":          result.Flagged,
				"highest_category": result.HighestCategory,
				"highest_score":    result.HighestScore,
				"category_scores":  result.CategoryScores,
				"latency_ms":       result.LatencyMS,
				"api_key_masked":   result.APIKeyMasked,
			},
			"keys": service.CMClient.Inspect(cfg),
		},
	})
}

// POST /api/admin/content_moderation/preview
func PreviewContentModeration(c *gin.Context) {
	var input struct {
		Text   string   `json:"text"`
		Images []string `json:"images"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	cfg := setting.GetContentModerationSetting()
	result, err := service.CMClient.Call(c.Request.Context(), cfg, input.Text, input.Images)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"flagged":          result.Flagged,
			"highest_category": result.HighestCategory,
			"highest_score":    result.HighestScore,
			"category_scores":  result.CategoryScores,
			"thresholds":       cfg.Thresholds,
		},
	})
}

// GET /api/admin/content_moderation/logs
func ListContentModerationLogs(c *gin.Context) {
	filter := model.ContentModerationLogFilter{}
	if v := c.Query("user_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.UserId = &n
		}
	}
	if v := c.Query("token_id"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			filter.TokenId = &n
		}
	}
	if v := c.Query("flagged"); v != "" {
		b := v == "true" || v == "1"
		filter.Flagged = &b
	}
	if v := c.Query("layer"); v != "" {
		filter.Layer = v
	}
	if v := c.Query("start"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.StartTs = n
		}
	}
	if v := c.Query("end"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			filter.EndTs = n
		}
	}
	if v := c.Query("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Page = n
		}
	}
	if v := c.Query("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.PageSize = n
		}
	}
	rows, total, err := model.QueryContentModerationLogs(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"total": total,
			"items": rows,
		},
	})
}

// GET /api/admin/content_moderation/logs/:id
func GetContentModerationLog(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid id"})
		return
	}
	row, err := model.GetContentModerationLogById(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": row})
}

// DELETE /api/admin/content_moderation/flagged_hash
func DeleteContentModerationFlaggedHash(c *gin.Context) {
	var input struct {
		Hash string `json:"hash"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	if err := service.CMHashCache.Delete(c.Request.Context(), input.Hash); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GET /api/admin/content_moderation/flagged_hash/count
func GetContentModerationFlaggedHashCount(c *gin.Context) {
	n, err := service.CMHashCache.Count(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"count": n}})
}

// POST /api/admin/content_moderation/flagged_hash/clear
func ClearContentModerationFlaggedHashes(c *gin.Context) {
	if err := service.CMHashCache.Clear(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// POST /api/admin/content_moderation/unban/:user_id
func UnbanContentModerationUser(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user_id"})
		return
	}
	if err := service.CMAutoBan.ClearUserViolations(c.Request.Context(), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	// 同时把用户状态恢复为 Enabled
	if err := model.ContentModerationEnableUser(id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

// GET /api/admin/content_moderation/violation_count/:user_id
func GetContentModerationViolationCount(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("user_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid user_id"})
		return
	}
	cfg := setting.GetContentModerationSetting()
	count, err := service.CMViolationCounter.GetCount(c.Request.Context(), id, cfg.ViolationWindowHours*3600)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{
		"user_id": id,
		"count":   count,
		"window_hours": cfg.ViolationWindowHours,
		"threshold":    cfg.BanThreshold,
	}})
}

// --- helpers ---

func contentModerationConfigView(cfg setting.ContentModerationSetting) gin.H {
	masked := make([]string, 0, len(cfg.APIKeys))
	for _, k := range cfg.APIKeys {
		masked = append(masked, service.MaskContentModerationKey(k))
	}
	return gin.H{
		"enabled":                cfg.Enabled,
		"mode":                   cfg.Mode,
		"base_url":               cfg.BaseURL,
		"model":                  cfg.Model,
		"api_key_count":          len(cfg.APIKeys),
		"api_key_masks":          masked,
		"timeout_ms":             cfg.TimeoutMS,
		"retry_count":            cfg.RetryCount,
		"thresholds":             cfg.Thresholds,
		"sample_rate":            cfg.SampleRate,
		"input_scope":            cfg.InputScope,
		"pre_hash_check_enabled": cfg.PreHashCheckEnabled,
		"model_mode":             cfg.ModelMode,
		"model_list":             cfg.ModelList,
		"block_status":           cfg.BlockStatus,
		"block_message":          cfg.BlockMessage,
		"auto_ban_enabled":       cfg.AutoBanEnabled,
		"ban_threshold":          cfg.BanThreshold,
		"violation_window_hours": cfg.ViolationWindowHours,
		"email_on_hit":           cfg.EmailOnHit,
		"email_to_admin":         cfg.EmailToAdmin,
		"email_to_user":          cfg.EmailToUser,
		"worker_count":           cfg.WorkerCount,
		"queue_size":             cfg.QueueSize,
		"record_non_hits":        cfg.RecordNonHits,
		"hit_retention_days":     cfg.HitRetentionDays,
		"non_hit_retention_days": cfg.NonHitRetentionDays,
		"categories":             setting.ContentModerationCategories(),
	}
}

func normalizeContentModerationKeys(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, k := range in {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	return out
}

func mergeContentModerationThresholds(base, override map[string]float64) map[string]float64 {
	out := map[string]float64{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if v < 0 {
			v = 0
		}
		if v > 1 {
			v = 1
		}
		out[k] = v
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// persistContentModerationSetting 把整个 ContentModerationSetting 序列化进 option 表。
// 通过 config.GlobalConfig.SaveToDB 实现，键格式 "content_moderation.<field>"。
func persistContentModerationSetting() error {
	return persistConfigManagerToOption()
}

// persistConfigManagerToOption 调用 config.GlobalConfig.SaveToDB(model.UpdateOption)。
// 提取成单独函数避免直接在 controller 引入 config 包。
func persistConfigManagerToOption() error {
	// 使用 model.UpdateOption 把每个键写入 options 表
	cfg := setting.GetContentModerationSetting()
	view := contentModerationConfigView(cfg)
	for k, v := range view {
		// 仅持久化 setting 自身的字段，跳过派生字段
		switch k {
		case "api_key_count", "api_key_masks", "categories":
			continue
		}
		var raw string
		switch t := v.(type) {
		case string:
			raw = t
		case bool:
			raw = strconv.FormatBool(t)
		case int:
			raw = strconv.Itoa(t)
		case int64:
			raw = strconv.FormatInt(t, 10)
		default:
			b, err := common.Marshal(v)
			if err != nil {
				continue
			}
			raw = string(b)
		}
		key := "content_moderation." + k
		if err := model.UpdateOption(key, raw); err != nil {
			return err
		}
	}
	if b, err := common.Marshal(cfg.APIKeys); err == nil {
		_ = model.UpdateOption("content_moderation.api_keys", string(b))
	}
	return nil
}
