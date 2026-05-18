package service

import "regexp"

var (
	cmReEmail = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// 中国大陆 11 位手机号（1 开头）+ 通用 10-15 位电话号（含国际号码）
	cmRePhoneCN = regexp.MustCompile(`\b1[3-9]\d{9}\b`)
	cmRePhone   = regexp.MustCompile(`\+?\d{10,15}\b`)
	// 中国大陆 18 位身份证号（最后一位允许 X/x）
	cmReIDCardCN = regexp.MustCompile(`\b\d{17}[\dXx]\b`)
	// 信用卡号占位（16 位连续数字，避免误伤先放在身份证后）
	cmReCreditCard = regexp.MustCompile(`\b\d{16,19}\b`)
)

// redactContentModerationPII 保守替换疑似 PII。命中即整段替换为 <PII>。
//
// 替换顺序敏感：先长后短，避免子串先被替换后掩盖更长的 PII。
func redactContentModerationPII(text string) string {
	if text == "" {
		return ""
	}
	text = cmReEmail.ReplaceAllString(text, "<PII>")
	text = cmReIDCardCN.ReplaceAllString(text, "<PII>")
	text = cmReCreditCard.ReplaceAllString(text, "<PII>")
	text = cmRePhoneCN.ReplaceAllString(text, "<PII>")
	text = cmRePhone.ReplaceAllString(text, "<PII>")
	return text
}
