package service

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// ContentModerationWorkerStats 是 worker 池的运行时指标。
type ContentModerationWorkerStats struct {
	ActiveWorkers int
	IdleWorkers   int
	QueueLength   int
	QueueCapacity int
	Enqueued      int64
	Dropped       int64
	Processed     int64
	Errors        int64
}

// contentModerationWorkerJob 是入队的单个审核任务。
type contentModerationWorkerJob struct {
	enqueuedAt time.Time
	req        ContentModerationCheckRequest
}

// contentModerationWorkerPool 提供异步队列 + 动态 worker 数 + 优雅关闭。
type contentModerationWorkerPool struct {
	ch          chan contentModerationWorkerJob
	stopCh      chan struct{}
	wg          sync.WaitGroup
	active      int32
	enqueued    int64
	dropped     int64
	processed   int64
	errors      int64
	startedOnce sync.Once
	stoppedOnce sync.Once
}

// CMWorker 是包级 worker 池。Init 时启动。
var CMWorker = newContentModerationWorkerPool()

func newContentModerationWorkerPool() *contentModerationWorkerPool {
	return &contentModerationWorkerPool{
		ch:     make(chan contentModerationWorkerJob, setting.MaxContentModerationQueueSize),
		stopCh: make(chan struct{}),
	}
}

// StartContentModerationWorkers 启动 maxContentModerationWorkerCount 个常驻 goroutine。
// 每个 worker 顶部按 cfg.WorkerCount 自我休眠，实现"动态调节"。
func StartContentModerationWorkers() {
	CMWorker.startedOnce.Do(func() {
		for i := 0; i < setting.MaxContentModerationWorkerCount; i++ {
			CMWorker.wg.Add(1)
			id := i
			go CMWorker.runWorker(id)
		}
	})
}

// ShutdownContentModerationWorkers 关闭队列并等待最多 5s 让 worker 排空。
func ShutdownContentModerationWorkers() {
	CMWorker.stoppedOnce.Do(func() {
		close(CMWorker.stopCh)
		// 等待最多 5s
		done := make(chan struct{})
		go func() {
			CMWorker.wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			common.SysLog("content_moderation worker shutdown timeout, forcing exit")
		}
	})
}

// EnqueueContentModerationJob 非阻塞入队。队列满时 drop + 计数 +1 + Warn 日志。
func EnqueueContentModerationJob(req ContentModerationCheckRequest) {
	job := contentModerationWorkerJob{enqueuedAt: time.Now(), req: req}
	select {
	case CMWorker.ch <- job:
		atomic.AddInt64(&CMWorker.enqueued, 1)
	default:
		atomic.AddInt64(&CMWorker.dropped, 1)
		// 不每条都打日志（避免 log spam），按 1024 条采样
		if atomic.LoadInt64(&CMWorker.dropped)%1024 == 1 {
			common.SysLog("content_moderation queue full, dropping job (sampled)")
		}
	}
}

// InspectContentModerationWorkerStats 返回 worker 池快照。
func InspectContentModerationWorkerStats() ContentModerationWorkerStats {
	active := int(atomic.LoadInt32(&CMWorker.active))
	cap := setting.MaxContentModerationWorkerCount
	cfg := setting.GetContentModerationSetting()
	want := cfg.WorkerCount
	if want < 0 {
		want = 0
	}
	if want > cap {
		want = cap
	}
	idle := want - active
	if idle < 0 {
		idle = 0
	}
	return ContentModerationWorkerStats{
		ActiveWorkers: active,
		IdleWorkers:   idle,
		QueueLength:   len(CMWorker.ch),
		QueueCapacity: cap_(CMWorker.ch),
		Enqueued:      atomic.LoadInt64(&CMWorker.enqueued),
		Dropped:       atomic.LoadInt64(&CMWorker.dropped),
		Processed:     atomic.LoadInt64(&CMWorker.processed),
		Errors:        atomic.LoadInt64(&CMWorker.errors),
	}
}

// cap_ 解决 channel cap() 不能直接命名的命名冲突。
func cap_(ch chan contentModerationWorkerJob) int { return cap(ch) }

func (p *contentModerationWorkerPool) runWorker(id int) {
	defer p.wg.Done()
	for {
		// 动态 worker 调节：cfg.WorkerCount 之外的 worker 自我休眠
		cfg := setting.GetContentModerationSetting()
		if id >= cfg.WorkerCount || !cfg.Enabled {
			select {
			case <-p.stopCh:
				return
			case <-time.After(time.Second):
				continue
			}
		}
		select {
		case <-p.stopCh:
			return
		case job, ok := <-p.ch:
			if !ok {
				return
			}
			p.processOne(job)
		}
	}
}

func (p *contentModerationWorkerPool) processOne(job contentModerationWorkerJob) {
	atomic.AddInt32(&p.active, 1)
	defer atomic.AddInt32(&p.active, -1)

	defer func() {
		if r := recover(); r != nil {
			atomic.AddInt64(&p.errors, 1)
			common.SysLog("content_moderation worker panic recovered: " +
				toString(r) + "\n" + string(debug.Stack()))
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 计算 queue delay
	delayMs := int(time.Since(job.enqueuedAt).Milliseconds())
	_ = delayMs // queue delay 已通过 CheckObserveAsync → writeLog 传递时机有限；此处仅统计
	CMService.CheckObserveAsync(ctx, job.req)
	atomic.AddInt64(&p.processed, 1)
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case error:
		return t.Error()
	default:
		return ""
	}
}

// ResetContentModerationWorkerForTest 清零 worker 池供测试使用。仅供测试调用。
func ResetContentModerationWorkerForTest() {
	CMWorker = newContentModerationWorkerPool()
}
