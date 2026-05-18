package model

import (
	"time"

	"github.com/QuantumNous/new-api/common"
)

// ContentModerationLog 是内容审核命中/未命中的持久化记录。
//
// 三库通用约束：
//   - 主键由 GORM 管理，无 AUTO_INCREMENT / SERIAL 直写。
//   - CategoryScores / ThresholdSnapshot 用 TEXT 存 JSON（避免 PG 特有 JSONB）。
//   - CreatedAt 使用 unix 秒（int64），与 model.Log 保持一致。
//   - Bool 字段在 GORM 抽象下三库都安全。
type ContentModerationLog struct {
	Id        int64 `json:"id" gorm:"primaryKey"`
	RequestId string `json:"request_id" gorm:"size:128;default:'';index:idx_cm_request_id"`

	// 请求归属
	UserId    int    `json:"user_id" gorm:"default:0;index:idx_cm_user_id;index:idx_cm_user_created,priority:1"`
	Username  string `json:"username" gorm:"size:64;default:''"`
	TokenId   int    `json:"token_id" gorm:"default:0;index:idx_cm_token_id"`
	TokenName string `json:"token_name" gorm:"size:64;default:''"`
	Group     string `json:"group" gorm:"column:group;size:64;default:''"`
	Ip        string `json:"ip" gorm:"size:64;default:''"`

	// 路由信息
	Endpoint string `json:"endpoint" gorm:"size:128;default:''"`
	Provider string `json:"provider" gorm:"size:64;default:''"`
	Model    string `json:"model" gorm:"size:128;default:''"`
	Protocol string `json:"protocol" gorm:"size:64;default:''"`

	// 决策状态
	Mode           string `json:"mode" gorm:"size:32;default:''"`
	Action         string `json:"action" gorm:"size:32;default:''"`
	DetectionLayer string `json:"detection_layer" gorm:"size:32;default:'';index:idx_cm_detection_layer"`
	Flagged        bool   `json:"flagged" gorm:"default:false;index:idx_cm_flagged;index:idx_cm_flagged_created,priority:1"`

	// 命中详情
	HighestCategory   string `json:"highest_category" gorm:"size:64;default:''"`
	HighestScore      float64 `json:"highest_score" gorm:"default:0"`
	CategoryScores    string `json:"category_scores" gorm:"type:text"`
	ThresholdSnapshot string `json:"threshold_snapshot" gorm:"type:text"`

	// 输入摘要
	InputExcerpt string `json:"input_excerpt" gorm:"type:text"`
	InputHash    string `json:"input_hash" gorm:"size:64;default:'';index:idx_cm_input_hash"`

	// 上游 / 异步处理状态
	UpstreamLatencyMs int    `json:"upstream_latency_ms" gorm:"default:0"`
	QueueDelayMs      int    `json:"queue_delay_ms" gorm:"default:0"`
	Error             string `json:"error" gorm:"type:text"`

	// 触发的副作用
	ViolationCount int  `json:"violation_count" gorm:"default:0"`
	AutoBanned     bool `json:"auto_banned" gorm:"default:false"`
	EmailSent      bool `json:"email_sent" gorm:"default:false"`

	// 时间戳：unix 秒，与 model.Log 一致；同时索引 created_at 单列 + 复合
	CreatedAt int64 `json:"created_at" gorm:"index:idx_cm_created_at;index:idx_cm_user_created,priority:2;index:idx_cm_flagged_created,priority:2"`
}

func (ContentModerationLog) TableName() string {
	return "content_moderation_logs"
}

// CreateContentModerationLog 写入一条审核记录。
// CategoryScores / ThresholdSnapshot 字段调用方需提前序列化为 JSON 字符串。
func CreateContentModerationLog(log *ContentModerationLog) error {
	if log == nil {
		return nil
	}
	if log.CreatedAt == 0 {
		log.CreatedAt = time.Now().Unix()
	}
	return LOG_DB.Create(log).Error
}

// ContentModerationLogFilter 用于 admin 列表查询。
type ContentModerationLogFilter struct {
	UserId     *int
	TokenId    *int
	Flagged    *bool
	Layer      string
	StartTs    int64
	EndTs      int64
	Page       int
	PageSize   int
}

// QueryContentModerationLogs 按筛选条件返回分页结果。
func QueryContentModerationLogs(filter ContentModerationLogFilter) ([]ContentModerationLog, int64, error) {
	query := LOG_DB.Model(&ContentModerationLog{})
	if filter.UserId != nil {
		query = query.Where("user_id = ?", *filter.UserId)
	}
	if filter.TokenId != nil {
		query = query.Where("token_id = ?", *filter.TokenId)
	}
	if filter.Flagged != nil {
		query = query.Where("flagged = ?", *filter.Flagged)
	}
	if filter.Layer != "" {
		query = query.Where("detection_layer = ?", filter.Layer)
	}
	if filter.StartTs > 0 {
		query = query.Where("created_at >= ?", filter.StartTs)
	}
	if filter.EndTs > 0 {
		query = query.Where("created_at <= ?", filter.EndTs)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	pageSize := filter.PageSize
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 20
	}

	var rows []ContentModerationLog
	err := query.Order("created_at DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&rows).Error
	return rows, total, err
}

// GetContentModerationLogById 单条查询。
func GetContentModerationLogById(id int64) (*ContentModerationLog, error) {
	var row ContentModerationLog
	if err := LOG_DB.Where("id = ?", id).First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteContentModerationLogsBefore 按 flagged 状态分别清理 cutoff 之前的日志。
// 返回删除的行数。
func DeleteContentModerationLogsBefore(flagged bool, cutoff int64) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	res := LOG_DB.Where("flagged = ? AND created_at < ?", flagged, cutoff).
		Delete(&ContentModerationLog{})
	return res.RowsAffected, res.Error
}

// EncodeContentModerationJSONField 用 common.Marshal 将 map/slice 序列化为 TEXT 字段值。
// nil 或空 map 时返回 "{}" 以保持字段非空。
func EncodeContentModerationJSONField(v any) string {
	if v == nil {
		return "{}"
	}
	if m, ok := v.(map[string]float64); ok && len(m) == 0 {
		return "{}"
	}
	b, err := common.Marshal(v)
	if err != nil || len(b) == 0 {
		return "{}"
	}
	return string(b)
}

// DecodeContentModerationScores 反序列化 CategoryScores / ThresholdSnapshot。
func DecodeContentModerationScores(raw string) (map[string]float64, error) {
	if raw == "" {
		return map[string]float64{}, nil
	}
	out := map[string]float64{}
	if err := common.UnmarshalJsonStr(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

