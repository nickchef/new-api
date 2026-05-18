package service

import (
	"sync"
	"sync/atomic"
)

// CMMetrics 是 CM 模块的进程级监控指标。
// 通过 InspectContentModerationMetrics() 暴露给 status endpoint。
//
// 字段命名对齐 spec G14 中要求的 7 个指标：
//   - requests_total{layer,mode,action}
//   - latency_ms{layer} —— 这里只跟踪 OpenAI 调用 latency；本地词库太快忽略
//   - queue_length / queue_dropped_total / workers_active —— 由 worker pool 提供
//   - openai_errors_total{type}
//   - auto_bans_total
type cmMetrics struct {
	mu sync.Mutex

	// requests by layer + mode + action
	requests map[string]int64

	// latency 历史（envelope: sum + count → avg）
	openaiLatencySum   int64
	openaiLatencyCount int64

	// OpenAI 错误分类
	errorAuth      int64
	errorRateLimit int64
	errorTimeout   int64
	errorOther     int64

	// 自动封禁次数
	autoBansTotal int64
}

var cmGlobalMetrics = &cmMetrics{
	requests: map[string]int64{},
}

// CMMetricsRequestKey 构造 requests_total 的复合 label key。
func CMMetricsRequestKey(layer, mode, action string) string {
	return layer + "|" + mode + "|" + action
}

// RecordContentModerationRequest 在每次 service.Check 决策完成时记录一次。
func RecordContentModerationRequest(layer, mode, action string) {
	key := CMMetricsRequestKey(layer, mode, action)
	cmGlobalMetrics.mu.Lock()
	cmGlobalMetrics.requests[key]++
	cmGlobalMetrics.mu.Unlock()
}

// RecordContentModerationOpenAILatency 记录单次 OpenAI 调用 latency。
func RecordContentModerationOpenAILatency(latencyMS int) {
	if latencyMS < 0 {
		return
	}
	atomic.AddInt64(&cmGlobalMetrics.openaiLatencySum, int64(latencyMS))
	atomic.AddInt64(&cmGlobalMetrics.openaiLatencyCount, 1)
}

// RecordContentModerationOpenAIError 按类别累计错误数。
func RecordContentModerationOpenAIError(typ string) {
	switch typ {
	case "auth":
		atomic.AddInt64(&cmGlobalMetrics.errorAuth, 1)
	case "rate_limit":
		atomic.AddInt64(&cmGlobalMetrics.errorRateLimit, 1)
	case "timeout":
		atomic.AddInt64(&cmGlobalMetrics.errorTimeout, 1)
	default:
		atomic.AddInt64(&cmGlobalMetrics.errorOther, 1)
	}
}

// RecordContentModerationAutoBan 每次自动封禁触发时 +1。
func RecordContentModerationAutoBan() {
	atomic.AddInt64(&cmGlobalMetrics.autoBansTotal, 1)
}

// ContentModerationMetricsSnapshot 是面向 API 输出的 metrics 快照。
type ContentModerationMetricsSnapshot struct {
	RequestsTotal      map[string]int64 `json:"requests_total"`
	OpenAILatencyAvgMS float64          `json:"openai_latency_avg_ms"`
	OpenAILatencyCount int64            `json:"openai_latency_count"`
	OpenAIErrors       map[string]int64 `json:"openai_errors_total"`
	AutoBansTotal      int64            `json:"auto_bans_total"`
	Worker             ContentModerationWorkerStats `json:"worker"`
}

// InspectContentModerationMetrics 返回当前进程的指标快照。
func InspectContentModerationMetrics() ContentModerationMetricsSnapshot {
	cmGlobalMetrics.mu.Lock()
	reqs := make(map[string]int64, len(cmGlobalMetrics.requests))
	for k, v := range cmGlobalMetrics.requests {
		reqs[k] = v
	}
	cmGlobalMetrics.mu.Unlock()

	latencySum := atomic.LoadInt64(&cmGlobalMetrics.openaiLatencySum)
	latencyCount := atomic.LoadInt64(&cmGlobalMetrics.openaiLatencyCount)
	var avg float64
	if latencyCount > 0 {
		avg = float64(latencySum) / float64(latencyCount)
	}

	return ContentModerationMetricsSnapshot{
		RequestsTotal:      reqs,
		OpenAILatencyAvgMS: avg,
		OpenAILatencyCount: latencyCount,
		OpenAIErrors: map[string]int64{
			"auth":       atomic.LoadInt64(&cmGlobalMetrics.errorAuth),
			"rate_limit": atomic.LoadInt64(&cmGlobalMetrics.errorRateLimit),
			"timeout":    atomic.LoadInt64(&cmGlobalMetrics.errorTimeout),
			"other":      atomic.LoadInt64(&cmGlobalMetrics.errorOther),
		},
		AutoBansTotal: atomic.LoadInt64(&cmGlobalMetrics.autoBansTotal),
		Worker:        InspectContentModerationWorkerStats(),
	}
}

// ResetContentModerationMetricsForTest 仅供测试使用。
func ResetContentModerationMetricsForTest() {
	cmGlobalMetrics.mu.Lock()
	cmGlobalMetrics.requests = map[string]int64{}
	cmGlobalMetrics.mu.Unlock()
	atomic.StoreInt64(&cmGlobalMetrics.openaiLatencySum, 0)
	atomic.StoreInt64(&cmGlobalMetrics.openaiLatencyCount, 0)
	atomic.StoreInt64(&cmGlobalMetrics.errorAuth, 0)
	atomic.StoreInt64(&cmGlobalMetrics.errorRateLimit, 0)
	atomic.StoreInt64(&cmGlobalMetrics.errorTimeout, 0)
	atomic.StoreInt64(&cmGlobalMetrics.errorOther, 0)
	atomic.StoreInt64(&cmGlobalMetrics.autoBansTotal, 0)
}
