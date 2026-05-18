package service

import (
	"net/http"

	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// ContentModerationDecision 是中间件返回给 G7 适配器的判定快照。
type ContentModerationDecision struct {
	Action          string
	DetectionLayer  string
	HighestCategory string
	HighestScore    float64
	InputHash       string
	Message         string // 可选；为空时取 setting.BlockMessage
}

// WriteContentModerationBlockResponse 根据请求协议写出对应格式的错误响应并 Abort。
//
// HTTP 状态码：
//   - OpenAI 系列：默认 403（来自 setting.BlockStatus，可配）
//   - Claude / Gemini：固定 400 INVALID_ARGUMENT（与各家官方语义一致）
func WriteContentModerationBlockResponse(c *gin.Context, protocol string, decision ContentModerationDecision) {
	cfg := setting.GetContentModerationSetting()
	message := decision.Message
	if message == "" {
		message = cfg.BlockMessage
		if message == "" {
			message = setting.DefaultContentModerationBlockMessage
		}
	}

	switch protocol {
	case ContentModerationProtocolAnthropic:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"type": "error",
			"error": gin.H{
				"type":    "invalid_request_error",
				"message": message,
			},
		})
	case ContentModerationProtocolGemini:
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    400,
				"message": message,
				"status":  "INVALID_ARGUMENT",
			},
		})
	default:
		// OpenAI Chat / Responses / Images / MJ / Suno / Unknown 都走 OpenAI 错误格式
		status := cfg.BlockStatus
		if status < 400 || status > 599 {
			status = http.StatusForbidden
		}
		c.AbortWithStatusJSON(status, gin.H{
			"error": gin.H{
				"message": message,
				"type":    "content_policy_violation",
				"code":    "content_moderation_blocked",
			},
		})
	}
}
