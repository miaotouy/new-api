package model

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"gorm.io/gorm"
)

type LogQueryParams struct {
	MaxID             int
	UserID            *int
	LogType           int
	StartTimestamp    int64
	EndTimestamp      int64
	ModelName         string
	Username          string
	TokenName         string
	ChannelID         int
	Group             string
	RequestID         string
	UpstreamRequestID string
}

type LogQueryCursor struct {
	CreatedAt int64
	ID        int
	RequestID string
}

func GetLogExportSnapshotID(category string) (int64, error) {
	var snapshotID int64
	var tx *gorm.DB
	switch category {
	case "common":
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			return 0, nil
		}
		tx = LOG_DB.Model(&Log{})
	case "drawing":
		tx = DB.Model(&Midjourney{})
	case "task":
		tx = DB.Model(&Task{})
	default:
		return 0, nil
	}
	err := tx.Select("id").Order("id desc").Limit(1).Scan(&snapshotID).Error
	return snapshotID, err
}

func buildLogQuery(params LogQueryParams) (*gorm.DB, error) {
	tx := LOG_DB.Model(&Log{})
	if params.UserID != nil {
		tx = tx.Where("logs.user_id = ?", *params.UserID)
	}
	if params.MaxID > 0 && !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		tx = tx.Where("logs.id <= ?", params.MaxID)
	}
	if params.LogType != LogTypeUnknown {
		tx = tx.Where("logs.type = ?", params.LogType)
	}
	var err error
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", params.ModelName); err != nil {
		return nil, err
	}
	if params.UserID == nil {
		if tx, err = applyExplicitLogTextFilter(tx, "logs.username", params.Username); err != nil {
			return nil, err
		}
	}
	if params.TokenName != "" {
		tx = tx.Where("logs.token_name = ?", params.TokenName)
	}
	if params.RequestID != "" {
		tx = tx.Where("logs.request_id = ?", params.RequestID)
	}
	if params.UpstreamRequestID != "" {
		tx = tx.Where("logs.upstream_request_id = ?", params.UpstreamRequestID)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", params.EndTimestamp)
	}
	if params.UserID == nil && params.ChannelID != 0 {
		tx = tx.Where("logs.channel_id = ?", params.ChannelID)
	}
	if params.Group != "" {
		tx = tx.Where("logs."+logGroupCol+" = ?", params.Group)
	}
	return tx, nil
}

func CountLogs(params LogQueryParams) (int64, error) {
	tx, err := buildLogQuery(params)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.Count(&total).Error
	return total, err
}

func GetLogsForExport(params LogQueryParams, cursor *LogQueryCursor, limit int) ([]*Log, error) {
	tx, err := buildLogQuery(params)
	if err != nil {
		return nil, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
		if cursor != nil {
			tx = tx.Where("logs.created_at < ? OR (logs.created_at = ? AND logs.request_id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.RequestID)
		}
	} else if cursor != nil {
		tx = tx.Where("logs.created_at < ? OR (logs.created_at = ? AND logs.id < ?)", cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}
	var logs []*Log
	if err := tx.Order(order).Limit(limit).Find(&logs).Error; err != nil {
		return nil, err
	}
	if params.UserID != nil {
		for _, log := range logs {
			log.ChannelName = ""
			var otherMap map[string]any
			otherMap, _ = common.StrToMap(log.Other)
			if otherMap != nil {
				delete(otherMap, "admin_info")
				delete(otherMap, "audit_info")
				delete(otherMap, "stream_status")
			}
			log.Other = common.MapToJsonStr(otherMap)
		}
		return logs, nil
	}

	channelIDs := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIDs.Add(log.ChannelId)
		}
	}
	if channelIDs.Len() == 0 {
		return logs, nil
	}
	var channels []struct {
		ID   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if common.MemoryCacheEnabled {
		for _, channelID := range channelIDs.Items() {
			if channel, err := CacheGetChannel(channelID); err == nil {
				channels = append(channels, struct {
					ID   int    `gorm:"column:id"`
					Name string `gorm:"column:name"`
				}{ID: channelID, Name: channel.Name})
			}
		}
	} else if err := DB.Table("channels").Select("id, name").Where("id IN ?", channelIDs.Items()).Find(&channels).Error; err != nil {
		return nil, err
	}
	channelNames := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelNames[channel.ID] = channel.Name
	}
	for _, log := range logs {
		log.ChannelName = channelNames[log.ChannelId]
	}
	return logs, nil
}

func buildMidjourneyTaskQuery(userID *int, params TaskQueryParams) *gorm.DB {
	tx := DB.Model(&Midjourney{})
	if params.MaxID > 0 {
		tx = tx.Where("id <= ?", params.MaxID)
	}
	if userID != nil {
		tx = tx.Where("user_id = ?", *userID)
	} else if params.ChannelID != "" {
		tx = tx.Where("channel_id = ?", params.ChannelID)
	}
	if params.MjID != "" {
		tx = tx.Where("mj_id = ?", params.MjID)
	}
	if params.StartTimestamp != "" {
		tx = tx.Where("submit_time >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != "" {
		tx = tx.Where("submit_time <= ?", params.EndTimestamp)
	}
	return tx
}

func CountMidjourneyTasks(userID *int, params TaskQueryParams) (int64, error) {
	var total int64
	err := buildMidjourneyTaskQuery(userID, params).Count(&total).Error
	return total, err
}

func GetMidjourneyForExport(userID *int, params TaskQueryParams, beforeID int, limit int) ([]*Midjourney, error) {
	tx := buildMidjourneyTaskQuery(userID, params)
	if beforeID > 0 {
		tx = tx.Where("id < ?", beforeID)
	}
	var tasks []*Midjourney
	err := tx.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}

func buildSyncTaskQuery(userID *int, params SyncTaskQueryParams) *gorm.DB {
	tx := DB.Model(&Task{})
	if params.MaxID > 0 {
		tx = tx.Where("id <= ?", params.MaxID)
	}
	if userID != nil {
		tx = tx.Where("user_id = ?", *userID).Omit("channel_id")
	} else {
		if params.ChannelID != "" {
			tx = tx.Where("channel_id = ?", params.ChannelID)
		}
		if params.UserID != "" {
			tx = tx.Where("user_id = ?", params.UserID)
		}
		if len(params.UserIDs) != 0 {
			tx = tx.Where("user_id IN ?", params.UserIDs)
		}
	}
	if params.Platform != "" {
		tx = tx.Where("platform = ?", params.Platform)
	}
	if params.TaskID != "" {
		tx = tx.Where("task_id = ?", params.TaskID)
	}
	if params.Action != "" {
		tx = tx.Where("action = ?", params.Action)
	}
	if params.Status != "" {
		tx = tx.Where("status = ?", params.Status)
	}
	if params.StartTimestamp != 0 {
		tx = tx.Where("submit_time >= ?", params.StartTimestamp)
	}
	if params.EndTimestamp != 0 {
		tx = tx.Where("submit_time <= ?", params.EndTimestamp)
	}
	return tx
}

func CountSyncTasks(userID *int, params SyncTaskQueryParams) (int64, error) {
	var total int64
	err := buildSyncTaskQuery(userID, params).Count(&total).Error
	return total, err
}

func GetTasksForExport(userID *int, params SyncTaskQueryParams, beforeID int64, limit int) ([]*Task, error) {
	columns := []string{"id", "created_at", "updated_at", "task_id", "platform", "user_id", commonGroupCol, "quota", "action", "status", "fail_reason", "submit_time", "start_time", "finish_time", "progress", "properties", "data"}
	if userID == nil {
		columns = append(columns, "channel_id")
	}
	tx := buildSyncTaskQuery(userID, params).Select(columns)
	if beforeID > 0 {
		tx = tx.Where("id < ?", beforeID)
	}
	var tasks []*Task
	err := tx.Order("id desc").Limit(limit).Find(&tasks).Error
	return tasks, err
}
