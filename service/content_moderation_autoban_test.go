package service

import (
	"context"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

type fakeDisabler struct {
	mu      sync.Mutex
	users   map[int]int // userID -> status
	emails  map[int]string
	names   map[int]string
	calls   int
}

func newFakeDisabler() *fakeDisabler {
	return &fakeDisabler{
		users:  map[int]int{},
		emails: map[int]string{},
		names:  map[int]string{},
	}
}

func (f *fakeDisabler) GetUserStatus(id int) (int, string, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[id], f.names[id], f.emails[id], nil
}

func (f *fakeDisabler) DisableUser(id int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.users[id] = common.UserStatusDisabled
	f.calls++
	return nil
}

func TestAutoBan_BelowThreshold_NoBan(t *testing.T) {
	disabler := newFakeDisabler()
	disabler.users[1] = common.UserStatusEnabled
	a := &ContentModerationAutoBanService{
		counter:  NewMemoryViolationCounter(),
		disabler: disabler,
	}

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.AutoBanEnabled = true
	s.BanThreshold = 10
	s.ViolationWindowHours = 720
	setting.UnlockContentModeration()

	for i := 0; i < 5; i++ {
		count, banned := a.HandleViolation(context.Background(), 1, "req", "hate")
		require.Equal(t, int64(i+1), count)
		require.False(t, banned)
	}
	require.Equal(t, common.UserStatusEnabled, disabler.users[1])
	require.Equal(t, 0, disabler.calls)
}

func TestAutoBan_HitsThreshold_DisablesUser(t *testing.T) {
	disabler := newFakeDisabler()
	disabler.users[2] = common.UserStatusEnabled
	disabler.emails[2] = "u@x.com"
	disabler.names[2] = "alice"
	a := &ContentModerationAutoBanService{
		counter:  NewMemoryViolationCounter(),
		disabler: disabler,
	}

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.AutoBanEnabled = true
	s.BanThreshold = 3
	s.ViolationWindowHours = 720
	s.EmailOnHit = true
	setting.UnlockContentModeration()

	for i := 0; i < 3; i++ {
		_, banned := a.HandleViolation(context.Background(), 2, "req", "hate")
		if i < 2 {
			require.False(t, banned)
		} else {
			require.True(t, banned)
		}
	}
	require.Equal(t, common.UserStatusDisabled, disabler.users[2])
	require.Equal(t, 1, disabler.calls)

	// 第四次：已 disabled，不再调用 DisableUser
	a.HandleViolation(context.Background(), 2, "req", "hate")
	require.Equal(t, 1, disabler.calls, "不重复 disable 已 disabled 的用户")
}

func TestAutoBan_AutoBanDisabledInSetting(t *testing.T) {
	disabler := newFakeDisabler()
	disabler.users[3] = common.UserStatusEnabled
	a := &ContentModerationAutoBanService{
		counter:  NewMemoryViolationCounter(),
		disabler: disabler,
	}

	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	s.AutoBanEnabled = false
	s.BanThreshold = 1
	setting.UnlockContentModeration()
	t.Cleanup(func() {
		setting.LockContentModeration()
		setting.MutableContentModerationSetting().AutoBanEnabled = true
		setting.UnlockContentModeration()
	})

	a.HandleViolation(context.Background(), 3, "req", "hate")
	require.Equal(t, common.UserStatusEnabled, disabler.users[3])
}

func TestAutoBan_ClearUserViolations(t *testing.T) {
	counter := NewMemoryViolationCounter()
	_, _ = counter.IncrAndCount(context.Background(), 7, 3600)
	_, _ = counter.IncrAndCount(context.Background(), 7, 3600)
	a := &ContentModerationAutoBanService{counter: counter}

	cnt, _ := counter.GetCount(context.Background(), 7, 3600)
	require.Equal(t, int64(2), cnt)

	require.NoError(t, a.ClearUserViolations(context.Background(), 7))
	cnt, _ = counter.GetCount(context.Background(), 7, 3600)
	require.Equal(t, int64(0), cnt)
}
