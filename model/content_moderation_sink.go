package model

// ContentModerationLogRecord 与 service.ContentModerationLogRecord 字段对齐。
// 我们在 model 包里独立定义同名结构体，避免 model 反向依赖 service 包。
// 调用方（如 main.go 或 service 包）持有 service 的版本，转换后通过 sink 写入。
type ContentModerationLogRecord struct {
	RequestID         string
	UserID            int
	Username          string
	TokenID           int
	TokenName         string
	Group             string
	IP                string
	Endpoint          string
	Provider          string
	Model             string
	Protocol          string
	Mode              string
	Action            string
	DetectionLayer    string
	Flagged           bool
	HighestCategory   string
	HighestScore      float64
	CategoryScores    map[string]float64
	ThresholdSnapshot map[string]float64
	InputExcerpt      string
	InputHash         string
	UpstreamLatencyMs int
	QueueDelayMs      int
	Error             string
	ViolationCount    int
	AutoBanned        bool
	EmailSent         bool
	CreatedAt         int64
}

// PersistContentModerationLogRecord 把中间表示落库。
// 此函数是 service 与 model 解耦的唯一连接点。
func PersistContentModerationLogRecord(rec ContentModerationLogRecord) error {
	row := ContentModerationLog{
		RequestId:         rec.RequestID,
		UserId:            rec.UserID,
		Username:          rec.Username,
		TokenId:           rec.TokenID,
		TokenName:         rec.TokenName,
		Group:             rec.Group,
		Ip:                rec.IP,
		Endpoint:          rec.Endpoint,
		Provider:          rec.Provider,
		Model:             rec.Model,
		Protocol:          rec.Protocol,
		Mode:              rec.Mode,
		Action:            rec.Action,
		DetectionLayer:    rec.DetectionLayer,
		Flagged:           rec.Flagged,
		HighestCategory:   rec.HighestCategory,
		HighestScore:      rec.HighestScore,
		CategoryScores:    EncodeContentModerationJSONField(rec.CategoryScores),
		ThresholdSnapshot: EncodeContentModerationJSONField(rec.ThresholdSnapshot),
		InputExcerpt:      rec.InputExcerpt,
		InputHash:         rec.InputHash,
		UpstreamLatencyMs: rec.UpstreamLatencyMs,
		QueueDelayMs:      rec.QueueDelayMs,
		Error:             rec.Error,
		ViolationCount:    rec.ViolationCount,
		AutoBanned:        rec.AutoBanned,
		EmailSent:         rec.EmailSent,
		CreatedAt:         rec.CreatedAt,
	}
	return CreateContentModerationLog(&row)
}
