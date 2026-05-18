package service

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

func TestCleanup_RunOnce_CallsBothFlaggedAndUnflagged(t *testing.T) {
	var mu sync.Mutex
	type call struct {
		flagged bool
		cutoff  int64
	}
	var calls []call
	RegisterContentModerationCleaner(func(flagged bool, cutoff int64) (int64, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, call{flagged: flagged, cutoff: cutoff})
		return 1, nil
	})
	t.Cleanup(func() { RegisterContentModerationCleaner(nil) })

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.HitRetentionDays = 180
	s.NonHitRetentionDays = 3
	setting.UnlockContentModeration()

	now := time.Now().Unix()
	RunContentModerationCleanupOnce()

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, calls, 2)
	// 180d cutoff
	require.Equal(t, true, calls[0].flagged)
	require.InDelta(t, float64(now-180*86400), float64(calls[0].cutoff), 5)
	// 3d cutoff
	require.Equal(t, false, calls[1].flagged)
	require.InDelta(t, float64(now-3*86400), float64(calls[1].cutoff), 5)
}

func TestCleanup_NoCallerWhenCleanerNil(t *testing.T) {
	RegisterContentModerationCleaner(nil)
	// 不 panic 就行
	RunContentModerationCleanupOnce()
}

func TestCleanup_ZeroRetentionDaysSkipped(t *testing.T) {
	var called bool
	RegisterContentModerationCleaner(func(_ bool, _ int64) (int64, error) {
		called = true
		return 0, nil
	})
	t.Cleanup(func() { RegisterContentModerationCleaner(nil) })

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.HitRetentionDays = 0
	s.NonHitRetentionDays = 0
	setting.UnlockContentModeration()

	RunContentModerationCleanupOnce()
	require.False(t, called)
}

func TestInt64ToString(t *testing.T) {
	require.Equal(t, "0", int64ToString(0))
	require.Equal(t, "1", int64ToString(1))
	require.Equal(t, "123456", int64ToString(123456))
	require.Equal(t, "-42", int64ToString(-42))
}
