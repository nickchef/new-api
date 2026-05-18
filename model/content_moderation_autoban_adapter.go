package model

// ContentModerationUserDisablerImpl 是 service.ContentModerationUserDisabler 的 model 侧实现。
// main.go 注入此实例以避免 service → model 循环依赖。
type ContentModerationUserDisablerImpl struct{}

// NewContentModerationUserDisabler 构造默认实现。
func NewContentModerationUserDisabler() *ContentModerationUserDisablerImpl {
	return &ContentModerationUserDisablerImpl{}
}

// GetUserStatus 实现 service.ContentModerationUserDisabler。
func (ContentModerationUserDisablerImpl) GetUserStatus(userID int) (int, string, string, error) {
	return ContentModerationUserStatusAndProfile(userID)
}

// DisableUser 实现 service.ContentModerationUserDisabler。
func (ContentModerationUserDisablerImpl) DisableUser(userID int) error {
	return ContentModerationDisableUser(userID)
}
