package service

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/require"
)

// --- helpers ---

func extractLast(t *testing.T, protocol string, body string) ContentModerationInput {
	t.Helper()
	return ExtractContentModerationInput(protocol, []byte(body), setting.ContentModerationInputScopeLastUser)
}

// --- OpenAI Chat Completions ---

func TestExtract_OpenAIChat_LastUser_TextOnly(t *testing.T) {
	body := `{
		"messages":[
			{"role":"system","content":"you are helpful"},
			{"role":"user","content":"first question"},
			{"role":"assistant","content":"first answer"},
			{"role":"user","content":"second question"}
		]
	}`
	in := extractLast(t, ContentModerationProtocolOpenAIChat, body)
	require.Equal(t, "second question", in.Text)
	require.Len(t, in.Images, 0)
	require.NotEmpty(t, in.Hash)
}

func TestExtract_OpenAIChat_AllUser(t *testing.T) {
	body := `{
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"q1"},
			{"role":"assistant","content":"a1"},
			{"role":"user","content":"q2"}
		]
	}`
	in := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat,
		[]byte(body), setting.ContentModerationInputScopeAllUser)
	require.Contains(t, in.Text, "q1")
	require.Contains(t, in.Text, "q2")
	require.NotContains(t, in.Text, "a1")
	require.NotContains(t, in.Text, "sys")
}

func TestExtract_OpenAIChat_AllMessages(t *testing.T) {
	body := `{
		"messages":[
			{"role":"system","content":"sys"},
			{"role":"user","content":"q1"},
			{"role":"assistant","content":"a1"}
		]
	}`
	in := ExtractContentModerationInput(ContentModerationProtocolOpenAIChat,
		[]byte(body), setting.ContentModerationInputScopeAllMessages)
	require.Contains(t, in.Text, "sys")
	require.Contains(t, in.Text, "q1")
	require.Contains(t, in.Text, "a1")
}

func TestExtract_OpenAIChat_MultimodalImageURL(t *testing.T) {
	body := `{
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"describe this"},
				{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}
			]}
		]
	}`
	in := extractLast(t, ContentModerationProtocolOpenAIChat, body)
	require.Equal(t, "describe this", in.Text)
	require.Equal(t, []string{"https://example.com/a.png"}, in.Images)
}

func TestExtract_OpenAIChat_Base64DataURL(t *testing.T) {
	body := `{
		"messages":[
			{"role":"user","content":[
				{"type":"text","text":"x"},
				{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgo"}}
			]}
		]
	}`
	in := extractLast(t, ContentModerationProtocolOpenAIChat, body)
	require.Equal(t, "x", in.Text)
	require.Len(t, in.Images, 1)
	require.True(t, strings.HasPrefix(in.Images[0], "data:image/png;base64,"))
}

// --- Anthropic Messages ---

func TestExtract_Anthropic_LastUser_FiltersSystemReminder(t *testing.T) {
	body := `{
		"messages":[
			{"role":"user","content":"<system-reminder>hidden</system-reminder>"},
			{"role":"assistant","content":"yes?"},
			{"role":"user","content":[
				{"type":"text","text":"<system-reminder>ignore me</system-reminder>"},
				{"type":"text","text":"real question"},
				{"type":"image","source":{"media_type":"image/jpeg","data":"AAAA"}}
			]}
		]
	}`
	in := extractLast(t, ContentModerationProtocolAnthropic, body)
	require.Equal(t, "real question", in.Text)
	require.Len(t, in.Images, 1)
	require.True(t, strings.HasPrefix(in.Images[0], "data:image/jpeg;base64,AAAA"))
}

// --- OpenAI Responses ---

func TestExtract_OpenAIResponses_StringInput(t *testing.T) {
	body := `{"input":"just a string"}`
	in := extractLast(t, ContentModerationProtocolOpenAIResponses, body)
	require.Equal(t, "just a string", in.Text)
}

func TestExtract_OpenAIResponses_ArrayInput_LastUser(t *testing.T) {
	body := `{
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"first"}]},
			{"role":"assistant","content":[{"type":"output_text","text":"reply"}]},
			{"role":"user","content":[{"type":"input_text","text":"latest"}]}
		]
	}`
	in := extractLast(t, ContentModerationProtocolOpenAIResponses, body)
	require.Equal(t, "latest", in.Text)
}

// --- Gemini ---

func TestExtract_Gemini_LastUserWithInlineData(t *testing.T) {
	body := `{
		"contents":[
			{"role":"user","parts":[{"text":"earlier question"}]},
			{"role":"model","parts":[{"text":"earlier answer"}]},
			{"role":"user","parts":[
				{"text":"current"},
				{"inline_data":{"mime_type":"image/png","data":"YWFh"}}
			]}
		]
	}`
	in := extractLast(t, ContentModerationProtocolGemini, body)
	require.Equal(t, "current", in.Text)
	require.Equal(t, []string{"data:image/png;base64,YWFh"}, in.Images)
}

// --- OpenAI Images ---

func TestExtract_OpenAIImages_PromptOnly(t *testing.T) {
	body := `{"prompt":"a fantasy castle","model":"dall-e-3","n":1}`
	in := extractLast(t, ContentModerationProtocolOpenAIImages, body)
	require.Equal(t, "a fantasy castle", in.Text)
	require.Len(t, in.Images, 0)
}

// --- Midjourney ---

func TestExtract_Midjourney_Imagine(t *testing.T) {
	body := `{"prompt":"a cat","base64Array":["https://i.example.com/cat.jpg"],"action":"IMAGINE"}`
	in := extractLast(t, ContentModerationProtocolMidjourney, body)
	require.Equal(t, "a cat", in.Text)
	require.Equal(t, []string{"https://i.example.com/cat.jpg"}, in.Images)
}

// --- Suno ---

func TestExtract_Suno_Lyrics(t *testing.T) {
	body := `{"prompt":"sing","lyrics":"verse 1\nverse 2","tags":"pop"}`
	in := extractLast(t, ContentModerationProtocolSuno, body)
	require.Contains(t, in.Text, "sing")
	require.Contains(t, in.Text, "verse 1")
	require.Contains(t, in.Text, "pop")
}

// --- Hash stability ---

func TestExtract_HashStableAndOrderIndependent(t *testing.T) {
	body1 := `{"messages":[{"role":"user","content":[
		{"type":"text","text":"hello"},
		{"type":"image_url","image_url":{"url":"https://a/1.png"}},
		{"type":"image_url","image_url":{"url":"https://a/2.png"}}
	]}]}`
	body2 := `{"messages":[{"role":"user","content":[
		{"type":"image_url","image_url":{"url":"https://a/2.png"}},
		{"type":"text","text":"hello"},
		{"type":"image_url","image_url":{"url":"https://a/1.png"}}
	]}]}`
	a := extractLast(t, ContentModerationProtocolOpenAIChat, body1)
	b := extractLast(t, ContentModerationProtocolOpenAIChat, body2)
	require.Equal(t, a.Hash, b.Hash, "hash must be order-independent for same images")
}

func TestExtract_EmptyAndInvalid(t *testing.T) {
	require.True(t, ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, nil, "").IsEmpty())
	require.True(t, ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte("not json"), "").IsEmpty())
	require.True(t, ExtractContentModerationInput(ContentModerationProtocolOpenAIChat, []byte(`{"messages":[]}`), "").IsEmpty())
}

// --- Cross-protocol contamination (must NOT leak) ---

func TestExtract_OpenAIChat_IgnoresGeminiFields(t *testing.T) {
	body := `{
		"messages":[{"role":"user","content":"chat text"}],
		"contents":[{"role":"user","parts":[{"text":"gemini text"}]}]
	}`
	in := extractLast(t, ContentModerationProtocolOpenAIChat, body)
	require.Equal(t, "chat text", in.Text)
	require.NotContains(t, in.Text, "gemini text")
}

// --- Protocol-from-relay-format & from-path mapping ---

func TestProtocolFromPath(t *testing.T) {
	require.Equal(t, ContentModerationProtocolOpenAIChat, ContentModerationProtocolFromPath("/v1/chat/completions"))
	require.Equal(t, ContentModerationProtocolAnthropic, ContentModerationProtocolFromPath("/v1/messages"))
	require.Equal(t, ContentModerationProtocolGemini, ContentModerationProtocolFromPath("/v1beta/models/gemini-pro:generateContent"))
	require.Equal(t, ContentModerationProtocolOpenAIImages, ContentModerationProtocolFromPath("/v1/images/generations"))
	require.Equal(t, ContentModerationProtocolMidjourney, ContentModerationProtocolFromPath("/mj/submit/imagine"))
	require.Equal(t, ContentModerationProtocolSuno, ContentModerationProtocolFromPath("/suno/submit/music"))
}

// --- InputScope normalize ---

func TestNormalizeScopeFallback(t *testing.T) {
	require.Equal(t, setting.ContentModerationInputScopeLastUser, normalizeContentModerationScope(""))
	require.Equal(t, setting.ContentModerationInputScopeLastUser, normalizeContentModerationScope("garbage"))
	require.Equal(t, setting.ContentModerationInputScopeAllUser, normalizeContentModerationScope("all_user"))
	require.Equal(t, setting.ContentModerationInputScopeAllMessages, normalizeContentModerationScope("all_messages"))
}

// --- Normalization (whitespace collapse, rune trim) ---

func TestNormalizeContentModerationText_CollapsesWhitespace(t *testing.T) {
	require.Equal(t, "a b c", normalizeContentModerationText("  a   b\nc  "))
	require.Equal(t, "", normalizeContentModerationText("\n\t   "))
}

func TestTrimUnicodeRunes_HandlesUnicode(t *testing.T) {
	in := strings.Repeat("中", 100)
	out := trimUnicodeRunes(in, 5)
	require.Equal(t, 5, len([]rune(out)))
	require.Equal(t, "abc", trimUnicodeRunes("abc", 0))
	require.Equal(t, "abc", trimUnicodeRunes("abc", 10))
}

// --- Additional coverage ---

func TestProtocolFromRelayFormat(t *testing.T) {
	cases := []struct {
		in  types.RelayFormat
		out string
	}{
		{types.RelayFormatOpenAI, ContentModerationProtocolOpenAIChat},
		{types.RelayFormatOpenAIResponses, ContentModerationProtocolOpenAIResponses},
		{types.RelayFormatOpenAIResponsesCompaction, ContentModerationProtocolOpenAIResponses},
		{types.RelayFormatClaude, ContentModerationProtocolAnthropic},
		{types.RelayFormatGemini, ContentModerationProtocolGemini},
		{types.RelayFormatOpenAIImage, ContentModerationProtocolOpenAIImages},
		{types.RelayFormatMjProxy, ContentModerationProtocolMidjourney},
		{types.RelayFormatTask, ContentModerationProtocolSuno},
		{types.RelayFormatRerank, ContentModerationProtocolUnknown},
	}
	for _, c := range cases {
		require.Equal(t, c.out, ContentModerationProtocolFromRelayFormat(c.in), string(c.in))
	}
}

func TestExtract_Anthropic_AllUserAndAllMessages(t *testing.T) {
	body := `{
		"messages":[
			{"role":"user","content":"u1"},
			{"role":"assistant","content":"a1"},
			{"role":"user","content":"u2"}
		]
	}`
	allUser := ExtractContentModerationInput(ContentModerationProtocolAnthropic, []byte(body), setting.ContentModerationInputScopeAllUser)
	require.Contains(t, allUser.Text, "u1")
	require.Contains(t, allUser.Text, "u2")
	require.NotContains(t, allUser.Text, "a1")

	allMsg := ExtractContentModerationInput(ContentModerationProtocolAnthropic, []byte(body), setting.ContentModerationInputScopeAllMessages)
	require.Contains(t, allMsg.Text, "a1")
}

func TestExtract_Gemini_AllUserAndAllMessages(t *testing.T) {
	body := `{
		"contents":[
			{"role":"user","parts":[{"text":"q1"}]},
			{"role":"model","parts":[{"text":"m1"}]},
			{"role":"user","parts":[{"text":"q2"}]}
		]
	}`
	allUser := ExtractContentModerationInput(ContentModerationProtocolGemini, []byte(body), setting.ContentModerationInputScopeAllUser)
	require.Contains(t, allUser.Text, "q1")
	require.Contains(t, allUser.Text, "q2")
	require.NotContains(t, allUser.Text, "m1")

	allMsg := ExtractContentModerationInput(ContentModerationProtocolGemini, []byte(body), setting.ContentModerationInputScopeAllMessages)
	require.Contains(t, allMsg.Text, "m1")
}

func TestExtract_OpenAIResponses_AllUserAndAllMessages(t *testing.T) {
	body := `{
		"input":[
			{"role":"user","content":[{"type":"input_text","text":"x"}]},
			{"role":"assistant","content":[{"type":"output_text","text":"y"}]},
			{"role":"user","content":[{"type":"input_text","text":"z"}]}
		]
	}`
	allUser := ExtractContentModerationInput(ContentModerationProtocolOpenAIResponses, []byte(body), setting.ContentModerationInputScopeAllUser)
	require.Contains(t, allUser.Text, "x")
	require.Contains(t, allUser.Text, "z")
	require.NotContains(t, allUser.Text, "y")
}

func TestExtract_OpenAIResponses_ObjectInput(t *testing.T) {
	body := `{"input":{"role":"user","content":[{"type":"input_text","text":"obj"}]}}`
	in := extractLast(t, ContentModerationProtocolOpenAIResponses, body)
	require.Contains(t, in.Text, "obj")
}

func TestExtract_Default_UnknownProtocolFallsBack(t *testing.T) {
	body := `{"messages":[{"role":"user","content":"hello"}]}`
	in := extractLast(t, ContentModerationProtocolUnknown, body)
	require.Contains(t, in.Text, "hello")
}

func TestExtract_GeminiFileData(t *testing.T) {
	body := `{"contents":[{"role":"user","parts":[
		{"text":"check this"},
		{"file_data":{"file_uri":"https://files.example.com/x.png"}}
	]}]}`
	in := extractLast(t, ContentModerationProtocolGemini, body)
	require.Contains(t, in.Images, "https://files.example.com/x.png")
}

func TestExtract_ImageLimitDedup(t *testing.T) {
	body := `{"messages":[{"role":"user","content":[
		{"type":"image_url","image_url":{"url":"https://a/1.png"}},
		{"type":"image_url","image_url":{"url":"https://a/1.png"}},
		{"type":"image_url","image_url":{"url":"https://a/2.png"}},
		{"type":"image_url","image_url":{"url":"https://a/3.png"}},
		{"type":"image_url","image_url":{"url":"https://a/4.png"}},
		{"type":"image_url","image_url":{"url":"https://a/5.png"}},
		{"type":"image_url","image_url":{"url":"https://a/6.png"}},
		{"type":"image_url","image_url":{"url":"https://a/7.png"}},
		{"type":"image_url","image_url":{"url":"https://a/8.png"}},
		{"type":"image_url","image_url":{"url":"https://a/9.png"}}
	]}]}`
	in := extractLast(t, ContentModerationProtocolOpenAIChat, body)
	require.LessOrEqual(t, len(in.Images), setting.MaxContentModerationInputImages)
	// Dedup check: first two were duplicates → unique count should be 8 (limit cap may not bite)
	require.Equal(t, setting.MaxContentModerationInputImages, len(in.Images))
}

func TestExtract_Midjourney_ImageOnly(t *testing.T) {
	body := `{"base64Array":["https://i.example.com/cat.jpg","data:image/png;base64,YWFh"]}`
	in := extractLast(t, ContentModerationProtocolMidjourney, body)
	require.Equal(t, "", in.Text)
	require.Len(t, in.Images, 2)
}

func TestAddModerationText_SkipsSystemReminder(t *testing.T) {
	var parts []string
	addModerationText(&parts, "hello")
	addModerationText(&parts, "")
	addModerationText(&parts, "  with <system-reminder>x</system-reminder> filtered")
	require.Equal(t, []string{"hello"}, parts)
}
