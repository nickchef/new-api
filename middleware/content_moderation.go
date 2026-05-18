package middleware

import (
	"bytes"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ContentModeration 返回 CM 中间件。它在 controller 之前完成 L1 本地词库 + L2 OpenAI Moderation
// 双层检查。若命中则按请求协议格式 abort，否则放行。
//
// 中间件遵守"最小侵入"原则：不修改 controller/relay.go、service/sensitive.go、
// setting/sensitive.go；通过现有 common.GetBodyStorage 缓存机制保证 controller 仍能读 body。
func ContentModeration() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 模块未启用 → 快速放行
		cfg := setting.GetContentModerationSetting()
		l1Enabled := setting.ShouldCheckPromptSensitive()
		if !cfg.Enabled && !l1Enabled {
			c.Next()
			return
		}

		// 元数据请求 / 自身审核端点 → 放行
		path := c.Request.URL.Path
		if shouldSkipContentModerationPath(path) {
			c.Next()
			return
		}
		if c.Request.Method != "POST" && c.Request.Method != "PUT" {
			c.Next()
			return
		}

		bodyBytes, err := readContentModerationBody(c)
		if err != nil || len(bodyBytes) == 0 {
			c.Next()
			return
		}

		protocol := contentModerationProtocolFromContext(c, path)
		model := gjson.GetBytes(bodyBytes, "model").String()

		// L1：本地敏感词
		if l1Enabled {
			combined := service.ExtractContentModerationInput(protocol, bodyBytes, cfg.InputScope)
			if combined.Text != "" {
				if contains, _ := service.CheckSensitiveText(combined.Text); contains {
					handleContentModerationL1Hit(c, protocol)
					return
				}
			}
		}

		// L2：CM 模块
		if cfg.Enabled && cfg.Mode != setting.ContentModerationModeOff {
			req := service.ContentModerationCheckRequest{
				RequestID: c.GetString("X-Request-Id"),
				UserID:    c.GetInt("id"),
				Username:  c.GetString("username"),
				TokenID:   c.GetInt("token_id"),
				TokenName: c.GetString("token_name"),
				Group:     c.GetString("group"),
				IP:        c.ClientIP(),
				Endpoint:  path,
				Model:     model,
				Protocol:  protocol,
				Body:      bodyBytes,
			}
			res := service.CMService.Check(c.Request.Context(), req)
			switch res.Action {
			case setting.ContentModerationActionBlock, setting.ContentModerationActionHashBlock:
				service.WriteContentModerationBlockResponse(c, protocol, service.ContentModerationDecision{
					Action:          res.Action,
					DetectionLayer:  res.DetectionLayer,
					HighestCategory: res.HighestCategory,
					HighestScore:    res.HighestScore,
					InputHash:       res.InputHash,
				})
				return
			}
		}

		c.Next()
	}
}

// shouldSkipContentModerationPath 判定是否跳过审核。
//
// 跳过：模型元数据 / OpenAI 自审核 / 文件下载 / 健康检查 / WebSocket。
func shouldSkipContentModerationPath(path string) bool {
	low := strings.ToLower(path)
	if strings.Contains(low, "/v1/moderations") {
		return true
	}
	if strings.Contains(low, "/v1/models") || strings.Contains(low, "/v1beta/models") {
		// 但 /v1beta/models/<model>:generateContent 是审核目标
		if !strings.Contains(low, ":generatecontent") && !strings.Contains(low, ":streamgeneratecontent") {
			return true
		}
	}
	if strings.Contains(low, "/files") || strings.Contains(low, "/health") {
		return true
	}
	if strings.Contains(low, "/realtime") {
		return true
	}
	return false
}

// readContentModerationBody 读取请求 body 并通过 common.GetBodyStorage 缓存，
// 保证 controller 后续仍能读取。返回 nil 时调用方应直接放行。
func readContentModerationBody(c *gin.Context) ([]byte, error) {
	if c.Request.Body == nil {
		return nil, nil
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	// 重置 body 给后续 handler 读取
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

// contentModerationProtocolFromContext 按 URL path 推断。
//
// 注意：CM 中间件运行在 Distributor 之前（distributor 才设置 RelayFormat），
// 因此只能用 path 推断。如果未来 CM 中间件被放到 Distributor 之后，
// 可以读 c.Get("relay_format") 并通过 service.ContentModerationProtocolFromRelayFormat 转换。
func contentModerationProtocolFromContext(_ *gin.Context, path string) string {
	return service.ContentModerationProtocolFromPath(path)
}

// handleContentModerationL1Hit 当本地敏感词命中时写日志并返回错误响应。
func handleContentModerationL1Hit(c *gin.Context, protocol string) {
	service.WriteContentModerationBlockResponse(c, protocol, service.ContentModerationDecision{
		Action:         setting.ContentModerationActionBlock,
		DetectionLayer: setting.ContentModerationLayerLocal,
		Message:        setting.DefaultContentModerationBlockMessage,
	})
}
