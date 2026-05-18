package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting"

	"github.com/stretchr/testify/require"
)

func TestWorker_EnqueueAndDropOnFull(t *testing.T) {
	ResetContentModerationWorkerForTest()
	// 不启动 worker：所有 job 都积压
	for i := 0; i < 10; i++ {
		EnqueueContentModerationJob(ContentModerationCheckRequest{UserID: i})
	}
	stats := InspectContentModerationWorkerStats()
	require.Equal(t, int64(10), stats.Enqueued)
	require.Equal(t, int64(0), stats.Dropped)
	require.Equal(t, 10, stats.QueueLength)
}

func TestWorker_DropWhenFull(t *testing.T) {
	ResetContentModerationWorkerForTest()
	// 直接灌满 channel
	for i := 0; i < setting.MaxContentModerationQueueSize; i++ {
		EnqueueContentModerationJob(ContentModerationCheckRequest{})
	}
	// 再多塞一个会被 drop
	EnqueueContentModerationJob(ContentModerationCheckRequest{})
	stats := InspectContentModerationWorkerStats()
	require.Equal(t, int64(1), stats.Dropped)
}

func TestWorker_ProcessesJob(t *testing.T) {
	ResetContentModerationWorkerForTest()
	// 拦截 CheckObserveAsync：注入一个仅计数的 sink
	prevSvc := CMService
	var processed int32
	CMService = &ContentModerationService{
		logSink: testLogSinkFunc(func() { atomic.AddInt32(&processed, 1) }),
	}
	t.Cleanup(func() { CMService = prevSvc })

	// 启动 worker
	setting.LockContentModeration()
	setting.MutableContentModerationSetting().Enabled = true
	setting.MutableContentModerationSetting().Mode = setting.ContentModerationModeObserve
	setting.MutableContentModerationSetting().WorkerCount = 2
	setting.UnlockContentModeration()

	StartContentModerationWorkers()
	t.Cleanup(ShutdownContentModerationWorkers)

	EnqueueContentModerationJob(ContentModerationCheckRequest{
		UserID:   1,
		Protocol: ContentModerationProtocolOpenAIChat,
		Body:     []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
	})
	// 等最多 2s 让 worker 跑
	require.Eventually(t, func() bool {
		s := InspectContentModerationWorkerStats()
		return s.Processed >= 1
	}, 2*time.Second, 20*time.Millisecond)
}

type testLogSinkFunc func()

func (f testLogSinkFunc) Write(_ context.Context, _ ContentModerationLogRecord) {
	f()
}
