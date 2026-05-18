package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newRecorderCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	return ctx, rec
}

func TestWriteBlockResponse_OpenAI(t *testing.T) {
	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolOpenAIChat, ContentModerationDecision{
		Action:  setting.ContentModerationActionBlock,
		Message: "blocked by policy",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), `"content_policy_violation"`)
	require.Contains(t, rec.Body.String(), "content_moderation_blocked")
	require.Contains(t, rec.Body.String(), "blocked by policy")
	require.True(t, ctx.IsAborted())
}

func TestWriteBlockResponse_OpenAIImages_AlsoOpenAIFormat(t *testing.T) {
	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolOpenAIImages, ContentModerationDecision{
		Message: "nope",
	})
	require.Equal(t, http.StatusForbidden, rec.Code)
	require.Contains(t, rec.Body.String(), "content_policy_violation")
}

func TestWriteBlockResponse_Anthropic(t *testing.T) {
	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolAnthropic, ContentModerationDecision{
		Message: "claude blocked",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "claude blocked")
}

func TestWriteBlockResponse_Gemini(t *testing.T) {
	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolGemini, ContentModerationDecision{
		Message: "gemini blocked",
	})
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "INVALID_ARGUMENT")
	require.Contains(t, rec.Body.String(), "gemini blocked")
}

func TestWriteBlockResponse_DefaultMessageFromSetting(t *testing.T) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	prev := s.BlockMessage
	s.BlockMessage = "custom-from-setting"
	setting.UnlockContentModeration()
	t.Cleanup(func() {
		setting.LockContentModeration()
		setting.MutableContentModerationSetting().BlockMessage = prev
		setting.UnlockContentModeration()
	})

	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolOpenAIChat, ContentModerationDecision{})
	require.Contains(t, rec.Body.String(), "custom-from-setting")
}

func TestWriteBlockResponse_InvalidStatusFallsBack(t *testing.T) {
	setting.LockContentModeration()
	s := setting.MutableContentModerationSetting()
	prev := s.BlockStatus
	s.BlockStatus = 200 // invalid
	setting.UnlockContentModeration()
	t.Cleanup(func() {
		setting.LockContentModeration()
		setting.MutableContentModerationSetting().BlockStatus = prev
		setting.UnlockContentModeration()
	})

	ctx, rec := newRecorderCtx()
	WriteContentModerationBlockResponse(ctx, ContentModerationProtocolOpenAIChat, ContentModerationDecision{Message: "x"})
	require.Equal(t, http.StatusForbidden, rec.Code)
}
