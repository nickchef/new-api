package service

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationUserDisabler 抽象 model.User 的状态更新与查询，供 G11 注入。
type ContentModerationUserDisabler interface {
	GetUserStatus(userID int) (int, string, string, error) // status, username, email
	DisableUser(userID int) error
}

// ContentModerationAutoBanService 把 ViolationCounter + UserDisabler + Email 串成一个闭环。
type ContentModerationAutoBanService struct {
	counter  ContentModerationViolationCounter
	disabler ContentModerationUserDisabler
}

// CMAutoBan 是包级单例。Init 时注入。
var CMAutoBan = &ContentModerationAutoBanService{}

// InitContentModerationAutoBan 在 main.go 启动时调用。
func InitContentModerationAutoBan(disabler ContentModerationUserDisabler) {
	CMAutoBan.counter = CMViolationCounter
	CMAutoBan.disabler = disabler
}

// HandleViolation 是 service.Check / worker 命中违规后的回调。
//
// 流程：
//  1. ViolationCounter.IncrAndCount(userID, window)
//  2. 写到 result/log 上下文：rec.ViolationCount
//  3. 若 count >= BanThreshold 且 user 未 disabled → DisableUser
//  4. 触发邮件（24h 限频）
func (a *ContentModerationAutoBanService) HandleViolation(
	ctx context.Context,
	userID int,
	requestID, highestCategory string,
) (count int64, autoBanned bool) {
	if a == nil || userID <= 0 {
		return 0, false
	}
	cfg := setting.GetContentModerationSetting()

	counter := a.counter
	if counter == nil {
		counter = CMViolationCounter
	}
	if counter != nil {
		windowSec := cfg.ViolationWindowHours * 3600
		if windowSec <= 0 {
			windowSec = setting.DefaultContentModerationViolationWindowHours * 3600
		}
		c, err := counter.IncrAndCount(ctx, userID, windowSec)
		if err != nil {
			common.SysLog("content_moderation: incr violations failed: " + err.Error())
		}
		count = c
	}

	if cfg.AutoBanEnabled && cfg.BanThreshold > 0 && count >= int64(cfg.BanThreshold) && a.disabler != nil {
		status, username, email, err := a.disabler.GetUserStatus(userID)
		if err == nil && status != common.UserStatusDisabled {
			if derr := a.disabler.DisableUser(userID); derr != nil {
				common.SysLog("content_moderation: disable user failed: " + derr.Error())
			} else {
				autoBanned = true
				RecordContentModerationAutoBan()
			}
		}
		// 即便用户已 disabled，仍可触发邮件（受 24h 限频限制）
		_, _, _ = SendContentModerationAutoBanEmail(ctx, ContentModerationEmailEvent{
			UserID:          userID,
			UserEmail:       email,
			Username:        username,
			HighestCategory: highestCategory,
			ViolationCount:  int(count),
			HappenedAt:      time.Now(),
			RequestID:       requestID,
			AutoBanned:      autoBanned || status == common.UserStatusDisabled,
		})
	}
	return count, autoBanned
}

// ClearUserViolations 解封时调用，清空滑窗。
func (a *ContentModerationAutoBanService) ClearUserViolations(ctx context.Context, userID int) error {
	if a == nil || a.counter == nil {
		return nil
	}
	return a.counter.Clear(ctx, userID)
}
