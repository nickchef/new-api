package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetrics_RecordsAndAggregates(t *testing.T) {
	ResetContentModerationMetricsForTest()
	RecordContentModerationRequest("openai_moderation", "pre_block", "allow")
	RecordContentModerationRequest("openai_moderation", "pre_block", "allow")
	RecordContentModerationRequest("openai_moderation", "pre_block", "block")
	RecordContentModerationOpenAILatency(100)
	RecordContentModerationOpenAILatency(300)
	RecordContentModerationOpenAIError("auth")
	RecordContentModerationOpenAIError("auth")
	RecordContentModerationOpenAIError("rate_limit")
	RecordContentModerationOpenAIError("timeout")
	RecordContentModerationOpenAIError("strange")
	RecordContentModerationAutoBan()

	m := InspectContentModerationMetrics()
	require.Equal(t, int64(2), m.RequestsTotal[CMMetricsRequestKey("openai_moderation", "pre_block", "allow")])
	require.Equal(t, int64(1), m.RequestsTotal[CMMetricsRequestKey("openai_moderation", "pre_block", "block")])
	require.InDelta(t, 200.0, m.OpenAILatencyAvgMS, 1e-6)
	require.Equal(t, int64(2), m.OpenAILatencyCount)
	require.Equal(t, int64(2), m.OpenAIErrors["auth"])
	require.Equal(t, int64(1), m.OpenAIErrors["rate_limit"])
	require.Equal(t, int64(1), m.OpenAIErrors["timeout"])
	require.Equal(t, int64(1), m.OpenAIErrors["other"])
	require.Equal(t, int64(1), m.AutoBansTotal)
}

func TestClassifyContentModerationError(t *testing.T) {
	require.Equal(t, "other", classifyContentModerationError(nil))
	require.Equal(t, "auth", classifyContentModerationError(errors.New("status 401 unauthorized")))
	require.Equal(t, "rate_limit", classifyContentModerationError(errors.New("429 rate exceeded")))
	require.Equal(t, "timeout", classifyContentModerationError(errors.New("context deadline exceeded")))
	require.Equal(t, "other", classifyContentModerationError(errors.New("boom")))
}

func TestMetrics_LatencyAvgZeroOnNoSamples(t *testing.T) {
	ResetContentModerationMetricsForTest()
	m := InspectContentModerationMetrics()
	require.Equal(t, 0.0, m.OpenAILatencyAvgMS)
	require.Equal(t, int64(0), m.OpenAILatencyCount)
}
