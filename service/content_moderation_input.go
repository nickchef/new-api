package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/tidwall/gjson"
)

// ContentModerationProtocol 是 CM 模块内部对协议的标识。
// 与 types.RelayFormat 通过 ContentModerationProtocolFromRelayFormat 建立映射。
const (
	ContentModerationProtocolOpenAIChat      = "openai_chat_completions"
	ContentModerationProtocolOpenAIResponses = "openai_responses"
	ContentModerationProtocolAnthropic       = "anthropic_messages"
	ContentModerationProtocolGemini          = "gemini"
	ContentModerationProtocolOpenAIImages    = "openai_images"
	ContentModerationProtocolMidjourney      = "midjourney"
	ContentModerationProtocolSuno            = "suno"
	ContentModerationProtocolUnknown         = "unknown"
)

// ContentModerationInput 是审核器消费的归一化输入。
type ContentModerationInput struct {
	Text     string
	Images   []string
	Hash     string
	Protocol string
}

// IsEmpty 报告是否无文本无图像。
func (in ContentModerationInput) IsEmpty() bool {
	return strings.TrimSpace(in.Text) == "" && len(in.Images) == 0
}

// ContentModerationProtocolFromRelayFormat 把 new-api 的 RelayFormat 映射到 CM 协议常量。
func ContentModerationProtocolFromRelayFormat(format types.RelayFormat) string {
	switch format {
	case types.RelayFormatOpenAI:
		return ContentModerationProtocolOpenAIChat
	case types.RelayFormatOpenAIResponses, types.RelayFormatOpenAIResponsesCompaction:
		return ContentModerationProtocolOpenAIResponses
	case types.RelayFormatClaude:
		return ContentModerationProtocolAnthropic
	case types.RelayFormatGemini:
		return ContentModerationProtocolGemini
	case types.RelayFormatOpenAIImage:
		return ContentModerationProtocolOpenAIImages
	case types.RelayFormatMjProxy:
		return ContentModerationProtocolMidjourney
	case types.RelayFormatTask:
		// suno/mj 等任务型协议在 distributor 之后才能区分，
		// 中间件层只能识别到 RelayFormatTask，进一步细化由路径决定。
		return ContentModerationProtocolSuno
	default:
		return ContentModerationProtocolUnknown
	}
}

// ContentModerationProtocolFromPath 在 distributor 尚未运行时按 URL path 推断协议。
func ContentModerationProtocolFromPath(path string) string {
	p := strings.ToLower(path)
	switch {
	case strings.Contains(p, "/v1/chat/completions"):
		return ContentModerationProtocolOpenAIChat
	case strings.Contains(p, "/v1/responses"):
		return ContentModerationProtocolOpenAIResponses
	case strings.Contains(p, "/v1/messages"):
		return ContentModerationProtocolAnthropic
	case strings.Contains(p, ":generatecontent"), strings.Contains(p, ":streamgeneratecontent"), strings.Contains(p, "/v1beta/models/"):
		return ContentModerationProtocolGemini
	case strings.Contains(p, "/v1/images/"):
		return ContentModerationProtocolOpenAIImages
	case strings.Contains(p, "/mj/submit"), strings.Contains(p, "/mj-"):
		return ContentModerationProtocolMidjourney
	case strings.Contains(p, "/suno/submit"):
		return ContentModerationProtocolSuno
	default:
		return ContentModerationProtocolUnknown
	}
}

// ExtractContentModerationInput 是主入口：按协议 + scope 从请求 body 中抽取审核输入。
//
//   - protocol：内部协议常量，见 ContentModerationProtocol*
//   - body：原始请求 JSON
//   - scope：last_user / all_user / all_messages，见 setting.ContentModerationInputScope*
func ExtractContentModerationInput(protocol string, body []byte, scope string) ContentModerationInput {
	out := ContentModerationInput{Protocol: protocol}
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return out
	}
	scope = normalizeContentModerationScope(scope)

	var parts []string
	var images []string
	switch protocol {
	case ContentModerationProtocolAnthropic:
		extractAnthropic(body, scope, &parts, &images)
	case ContentModerationProtocolOpenAIChat:
		extractOpenAIChat(body, scope, &parts, &images)
	case ContentModerationProtocolOpenAIResponses:
		extractOpenAIResponses(body, scope, &parts, &images)
	case ContentModerationProtocolGemini:
		extractGemini(body, scope, &parts, &images)
	case ContentModerationProtocolOpenAIImages:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		collectContentValue(gjson.GetBytes(body, "image"), &parts, &images)
		collectContentValue(gjson.GetBytes(body, "images"), &parts, &images)
	case ContentModerationProtocolMidjourney:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		addModerationText(&parts, gjson.GetBytes(body, "promptEn").String())
		addModerationText(&parts, gjson.GetBytes(body, "promptZh").String())
		collectImageOnly(gjson.GetBytes(body, "base64Array"), &images)
		collectImageOnly(gjson.GetBytes(body, "image"), &images)
	case ContentModerationProtocolSuno:
		addModerationText(&parts, gjson.GetBytes(body, "prompt").String())
		addModerationText(&parts, gjson.GetBytes(body, "lyrics").String())
		addModerationText(&parts, gjson.GetBytes(body, "gpt_description_prompt").String())
		addModerationText(&parts, gjson.GetBytes(body, "tags").String())
		addModerationText(&parts, gjson.GetBytes(body, "title").String())
	default:
		extractOpenAIResponses(body, scope, &parts, &images)
		extractOpenAIChat(body, scope, &parts, &images)
		extractGemini(body, scope, &parts, &images)
	}

	out.Text = trimUnicodeRunes(normalizeContentModerationText(strings.Join(parts, "\n")), setting.MaxContentModerationInputRunes)
	out.Images = normalizeAndLimitImages(images, setting.MaxContentModerationInputImages)
	out.Hash = computeContentModerationHash(out.Text, out.Images)
	return out
}

func normalizeContentModerationScope(scope string) string {
	switch scope {
	case setting.ContentModerationInputScopeAllUser,
		setting.ContentModerationInputScopeAllMessages:
		return scope
	default:
		return setting.ContentModerationInputScopeLastUser
	}
}

// === OpenAI Chat Completions ===

func extractOpenAIChat(body []byte, scope string, parts *[]string, images *[]string) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return
	}
	switch scope {
	case setting.ContentModerationInputScopeAllMessages:
		messages.ForEach(func(_, msg gjson.Result) bool {
			collectContentValue(msg.Get("content"), parts, images)
			return true
		})
	case setting.ContentModerationInputScopeAllUser:
		messages.ForEach(func(_, msg gjson.Result) bool {
			if msgRoleEquals(msg, "user") {
				collectContentValue(msg.Get("content"), parts, images)
			}
			return true
		})
	default:
		var lastParts []string
		var lastImages []string
		messages.ForEach(func(_, msg gjson.Result) bool {
			if msgRoleEquals(msg, "user") {
				var p []string
				var imgs []string
				collectContentValue(msg.Get("content"), &p, &imgs)
				if normalizeContentModerationText(strings.Join(p, "\n")) != "" || len(imgs) > 0 {
					lastParts = p
					lastImages = imgs
				}
			}
			return true
		})
		*parts = append(*parts, lastParts...)
		*images = append(*images, lastImages...)
	}
}

// === Anthropic Messages ===

func extractAnthropic(body []byte, scope string, parts *[]string, images *[]string) {
	messages := gjson.GetBytes(body, "messages")
	if !messages.IsArray() {
		return
	}
	switch scope {
	case setting.ContentModerationInputScopeAllMessages:
		messages.ForEach(func(_, msg gjson.Result) bool {
			collectAnthropicUserContentValue(msg.Get("content"), parts, images)
			return true
		})
	case setting.ContentModerationInputScopeAllUser:
		messages.ForEach(func(_, msg gjson.Result) bool {
			if msgRoleEquals(msg, "user") {
				collectAnthropicUserContentValue(msg.Get("content"), parts, images)
			}
			return true
		})
	default:
		var lastParts []string
		var lastImages []string
		messages.ForEach(func(_, msg gjson.Result) bool {
			if msgRoleEquals(msg, "user") {
				var p []string
				var imgs []string
				collectAnthropicUserContentValue(msg.Get("content"), &p, &imgs)
				if normalizeContentModerationText(strings.Join(p, "\n")) != "" || len(imgs) > 0 {
					lastParts = p
					lastImages = imgs
				}
			}
			return true
		})
		*parts = append(*parts, lastParts...)
		*images = append(*images, lastImages...)
	}
}

func collectAnthropicUserContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		if !isAnthropicSystemReminderText(value.String()) {
			addModerationText(parts, value.String())
		}
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectAnthropicUserContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		switch typ {
		case "", "text", "input_text", "message":
			if value.Get("text").Exists() && !isAnthropicSystemReminderText(value.Get("text").String()) {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectAnthropicUserContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
			collectContentValue(value, parts, images)
		}
	}
}

func isAnthropicSystemReminderText(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), "<system-reminder>")
}

// === OpenAI Responses (/v1/responses) ===

func extractOpenAIResponses(body []byte, scope string, parts *[]string, images *[]string) {
	input := gjson.GetBytes(body, "input")
	if !input.Exists() {
		return
	}
	switch input.Type {
	case gjson.String:
		addModerationText(parts, input.String())
		return
	}
	if input.IsArray() {
		switch scope {
		case setting.ContentModerationInputScopeAllMessages:
			input.ForEach(func(_, item gjson.Result) bool {
				collectResponsesItem(item, parts, images)
				return true
			})
		case setting.ContentModerationInputScopeAllUser:
			input.ForEach(func(_, item gjson.Result) bool {
				if isResponsesUserItem(item) {
					collectResponsesItem(item, parts, images)
				}
				return true
			})
		default:
			var last gjson.Result
			input.ForEach(func(_, item gjson.Result) bool {
				if isResponsesUserItem(item) {
					last = item
				}
				return true
			})
			if last.Exists() {
				collectResponsesItem(last, parts, images)
			}
		}
		return
	}
	if input.IsObject() && isResponsesUserItem(input) {
		collectResponsesItem(input, parts, images)
	}
}

func collectResponsesItem(item gjson.Result, parts *[]string, images *[]string) {
	collectContentValue(item.Get("content"), parts, images)
	if item.Get("type").String() == "input_text" || item.Get("text").Exists() {
		collectContentValue(item, parts, images)
	}
}

func isResponsesUserItem(item gjson.Result) bool {
	role := strings.ToLower(strings.TrimSpace(item.Get("role").String()))
	if role != "" && role != "user" {
		return false
	}
	var p []string
	var imgs []string
	collectResponsesItem(item, &p, &imgs)
	return normalizeContentModerationText(strings.Join(p, "\n")) != "" || len(imgs) > 0
}

// === Gemini ===

func extractGemini(body []byte, scope string, parts *[]string, images *[]string) {
	contents := gjson.GetBytes(body, "contents")
	if !contents.IsArray() {
		return
	}
	switch scope {
	case setting.ContentModerationInputScopeAllMessages:
		contents.ForEach(func(_, content gjson.Result) bool {
			collectGeminiContent(content, parts, images)
			return true
		})
	case setting.ContentModerationInputScopeAllUser:
		contents.ForEach(func(_, content gjson.Result) bool {
			role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
			if role == "" || role == "user" {
				collectGeminiContent(content, parts, images)
			}
			return true
		})
	default:
		var lastParts []string
		var lastImages []string
		contents.ForEach(func(_, content gjson.Result) bool {
			role := strings.ToLower(strings.TrimSpace(content.Get("role").String()))
			if role == "" || role == "user" {
				var p []string
				var imgs []string
				collectGeminiContent(content, &p, &imgs)
				if normalizeContentModerationText(strings.Join(p, "\n")) != "" || len(imgs) > 0 {
					lastParts = p
					lastImages = imgs
				}
			}
			return true
		})
		*parts = append(*parts, lastParts...)
		*images = append(*images, lastImages...)
	}
}

func collectGeminiContent(content gjson.Result, parts *[]string, images *[]string) {
	arr := content.Get("parts")
	if !arr.IsArray() {
		return
	}
	arr.ForEach(func(_, part gjson.Result) bool {
		addModerationText(parts, part.Get("text").String())
		addGeminiModerationImage(images, part)
		return true
	})
}

// === Generic content value collection (OpenAI Chat / image_url / base64) ===

func collectContentValue(value gjson.Result, parts *[]string, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationText(parts, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectContentValue(item, parts, images)
			return true
		})
	case value.IsObject():
		typ := strings.ToLower(strings.TrimSpace(value.Get("type").String()))
		addModerationImage(images, value.Get("image_url.url").String())
		addModerationImage(images, value.Get("image_url").String())
		addModerationImage(images, value.Get("url").String())
		addModerationImageData(images, value.Get("source.media_type").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("source.mediaType").String(), value.Get("source.data").String())
		addModerationImageData(images, value.Get("media_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mime_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mimeType").String(), value.Get("data").String())
		addModerationImage(images, value.Get("source.data").String())
		addModerationImage(images, value.Get("data").String())
		addModerationImage(images, value.Get("base64").String())
		switch typ {
		case "", "text", "input_text", "message":
			if value.Get("text").Exists() {
				addModerationText(parts, value.Get("text").String())
			}
			if value.Get("content").Exists() {
				collectContentValue(value.Get("content"), parts, images)
			}
		case "image_url", "input_image", "image":
		}
	}
}

// collectImageOnly 仅把字符串/对象解析为图像 URL/base64，不把字符串当作文本。
// 用于 MJ / Suno 等协议中已知字段语义就是图像数组的情况。
func collectImageOnly(value gjson.Result, images *[]string) {
	switch {
	case !value.Exists():
		return
	case value.Type == gjson.String:
		addModerationImage(images, value.String())
	case value.IsArray():
		value.ForEach(func(_, item gjson.Result) bool {
			collectImageOnly(item, images)
			return true
		})
	case value.IsObject():
		addModerationImage(images, value.Get("image_url.url").String())
		addModerationImage(images, value.Get("image_url").String())
		addModerationImage(images, value.Get("url").String())
		addModerationImage(images, value.Get("base64").String())
		addModerationImageData(images, value.Get("media_type").String(), value.Get("data").String())
		addModerationImageData(images, value.Get("mime_type").String(), value.Get("data").String())
	}
}

func addGeminiModerationImage(images *[]string, part gjson.Result) {
	if inlineData := part.Get("inline_data"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mime_type").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	if inlineData := part.Get("inlineData"); inlineData.IsObject() {
		mimeType := strings.TrimSpace(inlineData.Get("mimeType").String())
		data := strings.TrimSpace(inlineData.Get("data").String())
		if mimeType != "" && data != "" {
			addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
		}
	}
	addModerationImage(images, part.Get("file_data.file_uri").String())
	addModerationImage(images, part.Get("fileData.fileUri").String())
}

func addModerationImageData(images *[]string, mimeType string, data string) {
	mimeType = strings.TrimSpace(mimeType)
	data = strings.TrimSpace(data)
	if mimeType == "" || data == "" {
		return
	}
	addModerationImage(images, fmt.Sprintf("data:%s;base64,%s", mimeType, data))
}

func addModerationImage(images *[]string, image string) {
	image = strings.TrimSpace(image)
	if image == "" {
		return
	}
	if strings.HasPrefix(image, "data:") || strings.HasPrefix(image, "http://") || strings.HasPrefix(image, "https://") {
		*images = append(*images, image)
	}
}

func addModerationText(parts *[]string, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if strings.Contains(text, "<system-reminder>") {
		return
	}
	*parts = append(*parts, text)
}

func normalizeContentModerationText(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func trimUnicodeRunes(text string, max int) string {
	if max <= 0 {
		return text
	}
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max])
}

func normalizeAndLimitImages(images []string, max int) []string {
	out := make([]string, 0, len(images))
	seen := make(map[string]struct{}, len(images))
	for _, image := range images {
		image = strings.TrimSpace(image)
		if image == "" {
			continue
		}
		if _, ok := seen[image]; ok {
			continue
		}
		seen[image] = struct{}{}
		out = append(out, image)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func computeContentModerationHash(text string, images []string) string {
	sorted := make([]string, len(images))
	copy(sorted, images)
	sort.Strings(sorted)
	h := sha256.New()
	h.Write([]byte(text))
	for _, img := range sorted {
		h.Write([]byte{0})
		h.Write([]byte(img))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func msgRoleEquals(msg gjson.Result, want string) bool {
	return strings.ToLower(strings.TrimSpace(msg.Get("role").String())) == want
}
