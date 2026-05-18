package model

import "github.com/QuantumNous/new-api/common"

// ContentModerationUserStatusAndProfile 返回触发自动封禁需要的最小用户快照。
// 故意不返回 *User 整体，避免下游意外修改其它字段。
func ContentModerationUserStatusAndProfile(userID int) (status int, username, email string, err error) {
	row := struct {
		Status   int
		Username string
		Email    string
	}{}
	err = DB.Model(&User{}).
		Select("status", "username", "email").
		Where("id = ?", userID).
		First(&row).Error
	return row.Status, row.Username, row.Email, err
}

// ContentModerationDisableUser 把 user.status 置为 disabled。
// 单字段 update，避免 GORM Save 写回其它字段。
func ContentModerationDisableUser(userID int) error {
	return DB.Model(&User{}).
		Where("id = ?", userID).
		Update("status", common.UserStatusDisabled).Error
}

// ContentModerationEnableUser 把 user.status 置为 enabled。解封时使用。
func ContentModerationEnableUser(userID int) error {
	return DB.Model(&User{}).
		Where("id = ?", userID).
		Update("status", common.UserStatusEnabled).Error
}
