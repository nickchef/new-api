package service

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/common"
)

// ContentModerationLogPersister 把 service.ContentModerationLogRecord 转换并落库。
// 由 main.go 注入 model.PersistContentModerationLogRecord 适配函数。
type ContentModerationLogPersister func(rec ContentModerationLogRecord) error

// ContentModerationAutoBanner 触发用户自动封禁。由 G11 实现，main.go 注入。
// 已被 ContentModerationUserDisabler 取代，保留类型仅为向后兼容（暂未使用）。
type ContentModerationAutoBanner func(ctx context.Context, userID int) error

type adapterLogSink struct {
	persist ContentModerationLogPersister
}

func (a *adapterLogSink) Write(_ context.Context, rec ContentModerationLogRecord) {
	if a == nil || a.persist == nil {
		return
	}
	if err := a.persist(rec); err != nil {
		common.SysLog("content_moderation: persist log failed: " + err.Error())
	}
}

// InitContentModeration 是 main.go 唯一入口。它一次性完成：
//   - HashCache（Redis / 内存兜底）
//   - ViolationCounter（Redis / 内存兜底）
//   - OpenAI Client（共享 http.Client）
//   - Service 编排（LogSink + autoBan + worker enqueue）
//   - Worker 池启动
//   - 日志清理后台任务启动
//   - 自动封禁（通过 disabler 注入 model 实现）
//
// 调用方在进程退出时应调用 ShutdownContentModeration。
func InitContentModeration(
	persister ContentModerationLogPersister,
	disabler ContentModerationUserDisabler,
	cleaner ContentModerationCleaner,
) {
	InitContentModerationHashCache()
	InitContentModerationViolationCounter()
	InitContentModerationClient(&http.Client{})
	InitContentModerationAutoBan(disabler)

	sink := &adapterLogSink{persist: persister}
	autoBanCb := func(ctx context.Context, userID int, rec *ContentModerationLogRecord) {
		var requestID, highest string
		if rec != nil {
			requestID = rec.RequestID
			highest = rec.HighestCategory
		}
		CMAutoBan.HandleViolation(ctx, userID, requestID, highest)
	}
	InitContentModerationService(sink, EnqueueContentModerationJob, autoBanCb)
	StartContentModerationWorkers()
	if cleaner != nil {
		RegisterContentModerationCleaner(cleaner)
		StartContentModerationCleanupLoop()
	}
}

// ShutdownContentModeration 关闭 worker 池（带 5s 排空超时）并停止清理 goroutine。
func ShutdownContentModeration() {
	StopContentModerationCleanupLoop()
	ShutdownContentModerationWorkers()
}
