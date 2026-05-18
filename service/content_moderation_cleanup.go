package service

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationCleaner 由 model 包注入，负责按 flagged 状态删除 cutoff 之前的日志。
// 返回 (删除行数, error)。
type ContentModerationCleaner func(flagged bool, cutoff int64) (int64, error)

var (
	cmCleanerFn    ContentModerationCleaner
	cmCleanupMu    sync.Mutex
	cmCleanupStop  chan struct{}
	cmCleanupOnce  sync.Once
)

// RegisterContentModerationCleaner 由 main.go 注入 model 实现。
func RegisterContentModerationCleaner(fn ContentModerationCleaner) {
	cmCleanupMu.Lock()
	defer cmCleanupMu.Unlock()
	cmCleanerFn = fn
}

// StartContentModerationCleanupLoop 启动后台 goroutine：每天一次清理过期日志。
// 进程退出时通过 StopContentModerationCleanupLoop 终止。
func StartContentModerationCleanupLoop() {
	cmCleanupOnce.Do(func() {
		cmCleanupStop = make(chan struct{})
		go runContentModerationCleanupLoop()
	})
}

// StopContentModerationCleanupLoop 停止清理 goroutine。
func StopContentModerationCleanupLoop() {
	cmCleanupMu.Lock()
	defer cmCleanupMu.Unlock()
	if cmCleanupStop != nil {
		select {
		case <-cmCleanupStop:
		default:
			close(cmCleanupStop)
		}
	}
}

func runContentModerationCleanupLoop() {
	// 第一次启动后等待 30 分钟再跑，避免和启动期的其它后台任务竞争资源
	t := time.NewTimer(30 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-cmCleanupStop:
			return
		case <-t.C:
		}
		RunContentModerationCleanupOnce()
		t.Reset(24 * time.Hour)
	}
}

// RunContentModerationCleanupOnce 跑一次清理。可单独被管理 API 调用做手动清理。
func RunContentModerationCleanupOnce() {
	cmCleanupMu.Lock()
	fn := cmCleanerFn
	cmCleanupMu.Unlock()
	if fn == nil {
		return
	}
	cfg := setting.GetContentModerationSetting()
	now := time.Now().Unix()

	if cfg.HitRetentionDays > 0 {
		cutoff := now - int64(cfg.HitRetentionDays)*86400
		if deleted, err := fn(true, cutoff); err != nil {
			common.SysLog("content_moderation cleanup hit failed: " + err.Error())
		} else if deleted > 0 {
			common.SysLog("content_moderation cleanup hit: deleted=" +
				int64ToString(deleted))
		}
	}
	if cfg.NonHitRetentionDays > 0 {
		cutoff := now - int64(cfg.NonHitRetentionDays)*86400
		if deleted, err := fn(false, cutoff); err != nil {
			common.SysLog("content_moderation cleanup non-hit failed: " + err.Error())
		} else if deleted > 0 {
			common.SysLog("content_moderation cleanup non-hit: deleted=" +
				int64ToString(deleted))
		}
	}
}

func int64ToString(n int64) string {
	// 避免引入 strconv 仅为这一行
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [24]byte
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
