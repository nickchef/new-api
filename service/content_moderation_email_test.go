package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

type recordedEmail struct {
	subject, to, body string
}

func TestSendContentModerationAutoBanEmail_UserAndAdmin(t *testing.T) {
	var (
		mu      sync.Mutex
		emails  []recordedEmail
	)
	SetContentModerationEmailSender(func(subject, to, body string) error {
		mu.Lock()
		emails = append(emails, recordedEmail{subject: subject, to: to, body: body})
		mu.Unlock()
		return nil
	})
	t.Cleanup(ResetContentModerationEmailSender)

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.EmailOnHit = true
	s.EmailToUser = true
	s.EmailToAdmin = true
	setting.UnlockContentModeration()

	ev := ContentModerationEmailEvent{
		UserID:          42,
		UserEmail:       "user@example.com",
		Username:        "alice",
		HighestCategory: "hate",
		ViolationCount:  10,
		HappenedAt:      time.Now(),
		RequestID:       "req-001",
		AutoBanned:      true,
	}
	userSent, adminSent, err := SendContentModerationAutoBanEmail(context.Background(), ev)
	require.NoError(t, err)
	require.True(t, userSent)
	// 没有配置 SMTPFrom 时，admin 邮件应找不到收件人
	require.False(t, adminSent)

	require.Len(t, emails, 1)
	require.Equal(t, "user@example.com", emails[0].to)
	require.Contains(t, emails[0].body, "hate")
	require.Contains(t, emails[0].subject, "内容审核")
}

func TestSendContentModerationAutoBanEmail_DisabledOnHit(t *testing.T) {
	called := false
	SetContentModerationEmailSender(func(_, _, _ string) error {
		called = true
		return nil
	})
	t.Cleanup(ResetContentModerationEmailSender)

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.EmailOnHit = false
	setting.UnlockContentModeration()
	t.Cleanup(func() {
		setting.LockContentModeration()
		setting.MutableContentModerationSetting().EmailOnHit = true
		setting.UnlockContentModeration()
	})

	_, _, _ = SendContentModerationAutoBanEmail(context.Background(), ContentModerationEmailEvent{
		UserID:    1,
		UserEmail: "a@b.com",
	})
	require.False(t, called)
}

func TestBuildContentModerationUserEmail_NotAutoBanned(t *testing.T) {
	subject, body := buildContentModerationUserEmail(ContentModerationEmailEvent{
		UserID:          1,
		HighestCategory: "violence",
		ViolationCount:  3,
		HappenedAt:      time.Date(2026, 5, 18, 0, 0, 0, 0, time.UTC),
		AutoBanned:      false,
	})
	require.Contains(t, subject, "内容审核")
	require.Contains(t, body, "violence")
	require.Contains(t, body, "您最近的请求触发了内容审核策略")
	require.NotContains(t, body, "已被自动封禁")
}

func TestBuildContentModerationUserEmail_AutoBanned(t *testing.T) {
	_, body := buildContentModerationUserEmail(ContentModerationEmailEvent{
		AutoBanned: true,
	})
	require.Contains(t, body, "已被自动封禁")
}

func TestBuildContentModerationAdminEmail_HTMLEscape(t *testing.T) {
	_, body := buildContentModerationAdminEmail(ContentModerationEmailEvent{
		UserID:    1,
		Username:  "<script>alert(1)</script>",
		UserEmail: "x@y.z",
	})
	require.Contains(t, body, "&lt;script&gt;")
	require.NotContains(t, body, "<script>")
}

func TestRateLimit_WithoutRedis_IsFailOpen(t *testing.T) {
	// 没有 Redis 时直接放行
	require.True(t, contentModerationEmailRateLimitAcquire(context.Background(), 1))
}
