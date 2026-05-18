package service

import (
	"context"
	"fmt"
	"html"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationEmailEvent 是给 SendContentModerationAutoBanEmail 的输入快照。
// 故意不直接传 *model.ContentModerationLog，避免 service 包反向依赖 model 包。
type ContentModerationEmailEvent struct {
	UserID         int
	UserEmail      string
	Username       string
	HighestCategory string
	ViolationCount int
	HappenedAt     time.Time
	RequestID      string
	AutoBanned     bool
}

// EmailSender 是 SendEmail 的注入点，便于单测。
type EmailSender func(subject, receiver, content string) error

var defaultCMEmailSender EmailSender = common.SendEmail

// SetContentModerationEmailSender 替换底层邮件发送函数（仅供测试用）。
func SetContentModerationEmailSender(s EmailSender) {
	if s != nil {
		defaultCMEmailSender = s
	}
}

// ResetContentModerationEmailSender 恢复默认实现。
func ResetContentModerationEmailSender() {
	defaultCMEmailSender = common.SendEmail
}

// SendContentModerationAutoBanEmail 在用户违规触发自动封禁时通知用户 + 管理员。
// 24h 内同一 userID 只发一次（Redis SET NX EX 限频），Redis 不可达时降级为允许发送。
//
// 返回 (用户邮件已发, 管理员邮件已发, error)。任意一封失败不阻塞主链路。
func SendContentModerationAutoBanEmail(ctx context.Context, ev ContentModerationEmailEvent) (bool, bool, error) {
	cfg := setting.GetContentModerationSetting()
	if !cfg.EmailOnHit || ev.UserID <= 0 {
		return false, false, nil
	}

	// 24h 限频：Redis SET NX EX
	if !contentModerationEmailRateLimitAcquire(ctx, ev.UserID) {
		return false, false, nil
	}

	userSent := false
	adminSent := false

	if cfg.EmailToUser && strings.TrimSpace(ev.UserEmail) != "" {
		subject, body := buildContentModerationUserEmail(ev)
		if err := defaultCMEmailSender(subject, ev.UserEmail, body); err != nil {
			common.SysLog("content_moderation: user email failed: " + err.Error())
		} else {
			userSent = true
		}
	}

	if cfg.EmailToAdmin {
		adminEmail := getContentModerationAdminEmail()
		if adminEmail != "" {
			subject, body := buildContentModerationAdminEmail(ev)
			if err := defaultCMEmailSender(subject, adminEmail, body); err != nil {
				common.SysLog("content_moderation: admin email failed: " + err.Error())
			} else {
				adminSent = true
			}
		}
	}
	return userSent, adminSent, nil
}

// contentModerationEmailRateLimitAcquire 用 Redis SET NX EX 实现 24h 限频；
// Redis 不可达时返回 true（fail-open，允许发送）。
func contentModerationEmailRateLimitAcquire(ctx context.Context, userID int) bool {
	if !common.RedisEnabled || common.RDB == nil {
		return true
	}
	key := common.ContentModerationEmailSentKey(userID)
	ok, err := common.RDB.SetNX(ctx, key, "1", 24*time.Hour).Result()
	if err != nil {
		// Redis 出错时不阻塞邮件
		common.SysLog("content_moderation: email rate-limit SETNX failed: " + err.Error())
		return true
	}
	return ok
}

func buildContentModerationUserEmail(ev ContentModerationEmailEvent) (string, string) {
	system := common.SystemName
	if system == "" {
		system = "New-API"
	}
	subject := fmt.Sprintf("[%s] 内容审核提醒 / Content Moderation Notice", system)
	statusLine := "您最近的请求触发了内容审核策略。"
	if ev.AutoBanned {
		statusLine = "您的账户因连续触发内容审核策略已被自动封禁。"
	}
	body := fmt.Sprintf(`<p>%s</p>
<p>%s</p>
<ul>
<li>违规分类：%s</li>
<li>窗口内违规次数：%d</li>
<li>触发时间：%s</li>
</ul>
<p>如有疑问请联系站点管理员。</p>`,
		html.EscapeString(system),
		html.EscapeString(statusLine),
		html.EscapeString(ev.HighestCategory),
		ev.ViolationCount,
		ev.HappenedAt.Format(time.RFC3339))
	return subject, body
}

func buildContentModerationAdminEmail(ev ContentModerationEmailEvent) (string, string) {
	system := common.SystemName
	if system == "" {
		system = "New-API"
	}
	action := "命中"
	if ev.AutoBanned {
		action = "自动封禁"
	}
	subject := fmt.Sprintf("[%s] 内容审核告警：用户 %d %s / Content Moderation Alert",
		system, ev.UserID, action)
	body := fmt.Sprintf(`<p>站点：%s</p>
<p>用户 ID：%d（%s）</p>
<p>邮箱：%s</p>
<p>违规分类：%s</p>
<p>窗口内违规次数：%d</p>
<p>触发时间：%s</p>
<p>请求 ID：%s</p>
<p>当前动作：%s</p>`,
		html.EscapeString(system),
		ev.UserID,
		html.EscapeString(ev.Username),
		html.EscapeString(ev.UserEmail),
		html.EscapeString(ev.HighestCategory),
		ev.ViolationCount,
		ev.HappenedAt.Format(time.RFC3339),
		html.EscapeString(ev.RequestID),
		action)
	return subject, body
}

// getContentModerationAdminEmail 返回管理员邮箱（取 SMTPFrom 或 SystemName 邮箱占位）。
// 这里复用 common.SMTPFrom，与 new-api 现有邮件流向一致。
func getContentModerationAdminEmail() string {
	if email := strings.TrimSpace(common.SMTPFrom); email != "" {
		return email
	}
	return strings.TrimSpace(common.SMTPAccount)
}
