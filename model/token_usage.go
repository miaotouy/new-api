package model

import (
	"errors"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TokenUsageData stores hourly token metrics independently from quota_data.
type TokenUsageData struct {
	ID           int64  `json:"id" gorm:"primaryKey"`
	UserID       int    `json:"user_id" gorm:"index:idx_token_usage_user_hour,priority:1;index:idx_token_usage_user_model_hour,priority:1;uniqueIndex:idx_token_usage_bucket,priority:1"`
	Username     string `json:"username" gorm:"index:idx_token_usage_user_hour,priority:2;size:64;default:'';uniqueIndex:idx_token_usage_bucket,priority:2"`
	ModelName    string `json:"model_name" gorm:"index:idx_token_usage_model_hour,priority:1;index:idx_token_usage_user_model_hour,priority:2;uniqueIndex:idx_token_usage_bucket,priority:3;size:128;default:''"`
	CreatedAt    int64  `json:"created_at" gorm:"bigint;index:idx_token_usage_model_hour,priority:2;index:idx_token_usage_user_model_hour,priority:3;uniqueIndex:idx_token_usage_bucket,priority:4"`
	InputTokens  int    `json:"input_tokens" gorm:"default:0"`
	OutputTokens int    `json:"output_tokens" gorm:"default:0"`
	CachedTokens int    `json:"cached_tokens" gorm:"default:0"`
}

type TokenUsageDataItem struct {
	UserID       int    `json:"user_id,omitempty"`
	Username     string `json:"username,omitempty"`
	ModelName    string `json:"model_name,omitempty"`
	CreatedAt    int64  `json:"created_at"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	CachedTokens int    `json:"cached_tokens"`
	TokenUsed    int    `json:"token_used"`
}

type TokenUsageDataLogParams struct {
	UserID       int
	Username     string
	ModelName    string
	CreatedAt    int64
	InputTokens  int
	OutputTokens int
	CachedTokens int
}

type TokenUsageMigrationLog struct {
	ID               int
	UserID           int
	Username         string
	ModelName        string
	CreatedAt        int64
	PromptTokens     int
	CompletionTokens int
	Other            string
}

type TokenUsageLogCursor struct {
	AfterID int
	Offset  int
}

var (
	tokenUsageCache     = make(map[string]*TokenUsageData)
	tokenUsageCacheLock sync.Mutex
)

func tokenUsageHour(timestamp int64) int64 {
	return timestamp - timestamp%3600
}

func tokenUsageCacheKey(data *TokenUsageData) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%d", data.UserID, data.Username, data.ModelName, data.CreatedAt)
}

func LogTokenUsageData(params TokenUsageDataLogParams) {
	data := &TokenUsageData{
		UserID: params.UserID, Username: params.Username, ModelName: params.ModelName,
		CreatedAt: tokenUsageHour(params.CreatedAt), InputTokens: params.InputTokens,
		OutputTokens: params.OutputTokens, CachedTokens: params.CachedTokens,
	}
	if data.InputTokens == 0 && data.OutputTokens == 0 && data.CachedTokens == 0 {
		return
	}
	tokenUsageCacheLock.Lock()
	defer tokenUsageCacheLock.Unlock()
	key := tokenUsageCacheKey(data)
	if existing := tokenUsageCache[key]; existing != nil {
		existing.InputTokens += data.InputTokens
		existing.OutputTokens += data.OutputTokens
		existing.CachedTokens += data.CachedTokens
		return
	}
	tokenUsageCache[key] = data
}

// FlushTokenUsageDataCache persists pending token aggregates. It is called by
// the existing dashboard data exporter so token data follows its lifecycle.
func FlushTokenUsageDataCache() error {
	tokenUsageCacheLock.Lock()
	pending := tokenUsageCache
	tokenUsageCache = make(map[string]*TokenUsageData)
	tokenUsageCacheLock.Unlock()

	for key, data := range pending {
		if err := upsertTokenUsageData(data); err != nil {
			tokenUsageCacheLock.Lock()
			for pendingKey, pendingData := range pending {
				if current := tokenUsageCache[pendingKey]; current != nil {
					current.InputTokens += pendingData.InputTokens
					current.OutputTokens += pendingData.OutputTokens
					current.CachedTokens += pendingData.CachedTokens
					continue
				}
				tokenUsageCache[pendingKey] = pendingData
			}
			tokenUsageCacheLock.Unlock()
			return err
		}
		delete(pending, key)
	}
	return nil
}

func upsertTokenUsageData(data *TokenUsageData) error {
	var existing TokenUsageData
	err := DB.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
		data.UserID, data.Username, data.ModelName, data.CreatedAt).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		createErr := DB.Create(data).Error
		if createErr == nil {
			return nil
		}
		if err := DB.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
			data.UserID, data.Username, data.ModelName, data.CreatedAt).First(&existing).Error; err != nil {
			return createErr
		}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Model(&TokenUsageData{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"input_tokens":  gorm.Expr("input_tokens + ?", data.InputTokens),
		"output_tokens": gorm.Expr("output_tokens + ?", data.OutputTokens),
		"cached_tokens": gorm.Expr("cached_tokens + ?", data.CachedTokens),
	}).Error
}

func SetTokenUsageData(data *TokenUsageData) error {
	var existing TokenUsageData
	err := DB.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
		data.UserID, data.Username, data.ModelName, data.CreatedAt).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		createErr := DB.Create(data).Error
		if createErr == nil {
			return nil
		}
		if err := DB.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
			data.UserID, data.Username, data.ModelName, data.CreatedAt).First(&existing).Error; err != nil {
			return createErr
		}
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return DB.Model(&TokenUsageData{}).Where("id = ?", existing.ID).Updates(map[string]any{
		"input_tokens":  data.InputTokens,
		"output_tokens": data.OutputTokens,
		"cached_tokens": data.CachedTokens,
	}).Error
}

func GetTokenUsageData(startTime, endTime int64, username string) ([]*TokenUsageDataItem, error) {
	var data []*TokenUsageDataItem
	query := DB.Table("token_usage_data").Where("created_at >= ? AND created_at <= ?", startTime, endTime)
	if username != "" {
		query = query.Where("username = ?", username).Select(
			"user_id, username, model_name, created_at, sum(input_tokens) as input_tokens, sum(output_tokens) as output_tokens, sum(cached_tokens) as cached_tokens",
		).Group("user_id, username, model_name, created_at")
	} else {
		query = query.Select(
			"model_name, created_at, sum(input_tokens) as input_tokens, sum(output_tokens) as output_tokens, sum(cached_tokens) as cached_tokens",
		).Group("model_name, created_at")
	}
	err := query.Find(&data).Error
	for _, item := range data {
		item.TokenUsed = item.InputTokens + item.OutputTokens
	}
	return data, err
}

func GetUserTokenUsageData(userID int, startTime, endTime int64) ([]*TokenUsageDataItem, error) {
	var data []*TokenUsageDataItem
	err := DB.Table("token_usage_data").Select(
		"user_id, username, model_name, created_at, sum(input_tokens) as input_tokens, sum(output_tokens) as output_tokens, sum(cached_tokens) as cached_tokens",
	).Where("user_id = ? AND created_at >= ? AND created_at <= ?", userID, startTime, endTime).
		Group("user_id, username, model_name, created_at").Find(&data).Error
	for _, item := range data {
		item.TokenUsed = item.InputTokens + item.OutputTokens
	}
	return data, err
}

func CountConsumeLogsForTokenUsage(startTime, endTime int64) (int64, error) {
	var count int64
	err := LOG_DB.Model(&Log{}).Where("type = ? AND created_at >= ? AND created_at <= ?",
		LogTypeConsume, startTime, endTime).Count(&count).Error
	return count, err
}

func GetConsumeLogsForTokenUsage(startTime, endTime int64, cursor TokenUsageLogCursor, limit int) ([]*TokenUsageMigrationLog, TokenUsageLogCursor, error) {
	if limit <= 0 {
		limit = 500
	}
	var logs []*TokenUsageMigrationLog
	query := LOG_DB.Model(&Log{}).Select(
		"id, user_id, username, model_name, created_at, prompt_tokens, completion_tokens, other",
	).Where("type = ? AND created_at >= ? AND created_at <= ?", LogTypeConsume, startTime, endTime)
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		err := query.Order("created_at asc, request_id asc").Offset(cursor.Offset).Limit(limit).Find(&logs).Error
		if err != nil {
			return nil, cursor, err
		}
		cursor.Offset += len(logs)
		return logs, cursor, nil
	}
	err := query.Where("id > ?", cursor.AfterID).Order("id asc").Limit(limit).Find(&logs).Error
	if err != nil {
		return nil, cursor, err
	}
	if len(logs) > 0 {
		cursor.AfterID = logs[len(logs)-1].ID
	}
	return logs, cursor, nil
}
