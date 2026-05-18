package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// RegisterContentModerationRoutes 注册 CM admin 路由组到现有 /api 路由器下。
// 所有路由强制 AdminAuth，避免暴露给普通用户。
//
// 12 个 endpoints：
//   GET    /api/admin/content_moderation/config
//   PUT    /api/admin/content_moderation/config
//   GET    /api/admin/content_moderation/status
//   POST   /api/admin/content_moderation/test_api_keys
//   POST   /api/admin/content_moderation/preview
//   GET    /api/admin/content_moderation/logs
//   GET    /api/admin/content_moderation/logs/:id
//   DELETE /api/admin/content_moderation/flagged_hash
//   GET    /api/admin/content_moderation/flagged_hash/count
//   POST   /api/admin/content_moderation/flagged_hash/clear
//   POST   /api/admin/content_moderation/unban/:user_id
//   GET    /api/admin/content_moderation/violation_count/:user_id
func RegisterContentModerationRoutes(apiRouter *gin.RouterGroup) {
	g := apiRouter.Group("/admin/content_moderation")
	g.Use(middleware.AdminAuth())
	{
		g.GET("/config", controller.GetContentModerationConfig)
		g.PUT("/config", controller.UpdateContentModerationConfig)
		g.GET("/status", controller.GetContentModerationStatus)
		g.POST("/test_api_keys", controller.TestContentModerationAPIKeys)
		g.POST("/preview", controller.PreviewContentModeration)
		g.GET("/logs", controller.ListContentModerationLogs)
		g.GET("/logs/:id", controller.GetContentModerationLog)
		g.DELETE("/flagged_hash", controller.DeleteContentModerationFlaggedHash)
		g.GET("/flagged_hash/count", controller.GetContentModerationFlaggedHashCount)
		g.POST("/flagged_hash/clear", controller.ClearContentModerationFlaggedHashes)
		g.POST("/unban/:user_id", controller.UnbanContentModerationUser)
		g.GET("/violation_count/:user_id", controller.GetContentModerationViolationCount)
	}
}
