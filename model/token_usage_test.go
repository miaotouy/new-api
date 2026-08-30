package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTokenUsageTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&TokenUsageData{}))
	return db
}

func TestTokenUsageDataAggregatesByHourAndCorrectsExistingBucket(t *testing.T) {
	previousDB := DB
	t.Cleanup(func() { DB = previousDB })
	DB = newTokenUsageTestDB(t)

	timeInHour := int64(1_700_000_123)
	LogTokenUsageData(TokenUsageDataLogParams{
		UserID:       7,
		Username:     "alice",
		ModelName:    "model-a",
		CreatedAt:    timeInHour,
		InputTokens:  100,
		OutputTokens: 20,
		CachedTokens: 40,
	})
	LogTokenUsageData(TokenUsageDataLogParams{
		UserID:       7,
		Username:     "alice",
		ModelName:    "model-a",
		CreatedAt:    timeInHour + 1800,
		InputTokens:  50,
		OutputTokens: 10,
		CachedTokens: 5,
	})
	require.NoError(t, FlushTokenUsageDataCache())

	var stored TokenUsageData
	require.NoError(t, DB.First(&stored).Error)
	require.Equal(t, timeInHour-timeInHour%3600, stored.CreatedAt)
	require.Equal(t, 150, stored.InputTokens)
	require.Equal(t, 30, stored.OutputTokens)
	require.Equal(t, 45, stored.CachedTokens)

	data, err := GetUserTokenUsageData(7, stored.CreatedAt, stored.CreatedAt)
	require.NoError(t, err)
	require.Len(t, data, 1)
	require.Equal(t, 180, data[0].TokenUsed)

	require.NoError(t, SetTokenUsageData(&TokenUsageData{
		UserID:       7,
		Username:     "alice",
		ModelName:    "model-a",
		CreatedAt:    stored.CreatedAt,
		InputTokens:  80,
		OutputTokens: 12,
		CachedTokens: 30,
	}))
	require.NoError(t, SetTokenUsageData(&TokenUsageData{
		UserID:       7,
		Username:     "alice",
		ModelName:    "model-a",
		CreatedAt:    stored.CreatedAt,
		InputTokens:  80,
		OutputTokens: 12,
		CachedTokens: 30,
	}))

	require.NoError(t, DB.First(&stored).Error)
	require.Equal(t, 80, stored.InputTokens)
	require.Equal(t, 12, stored.OutputTokens)
	require.Equal(t, 30, stored.CachedTokens)
}

func TestTokenUsageDataKeepsBucketsSeparate(t *testing.T) {
	previousDB := DB
	t.Cleanup(func() { DB = previousDB })
	DB = newTokenUsageTestDB(t)

	base := int64(1_700_000_000)
	for _, item := range []TokenUsageData{
		{UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: base, InputTokens: 1},
		{UserID: 1, Username: "alice", ModelName: "model-b", CreatedAt: base, InputTokens: 2},
		{UserID: 2, Username: "bob", ModelName: "model-a", CreatedAt: base, InputTokens: 3},
	} {
		require.NoError(t, SetTokenUsageData(&item))
	}

	var count int64
	require.NoError(t, DB.Model(&TokenUsageData{}).Count(&count).Error)
	require.Equal(t, int64(3), count)
}

func TestGetTokenUsageDataAggregatesAllUsersByModelAndHour(t *testing.T) {
	previousDB := DB
	t.Cleanup(func() { DB = previousDB })
	DB = newTokenUsageTestDB(t)

	createdAt := int64(1_700_000_000)
	for _, item := range []TokenUsageData{
		{UserID: 1, Username: "alice", ModelName: "model-a", CreatedAt: createdAt, InputTokens: 100, OutputTokens: 20, CachedTokens: 40},
		{UserID: 2, Username: "bob", ModelName: "model-a", CreatedAt: createdAt, InputTokens: 30, OutputTokens: 5, CachedTokens: 10},
		{UserID: 1, Username: "alice", ModelName: "model-b", CreatedAt: createdAt, InputTokens: 50, OutputTokens: 10, CachedTokens: 0},
	} {
		require.NoError(t, SetTokenUsageData(&item))
	}

	data, err := GetTokenUsageData(createdAt, createdAt, "")
	require.NoError(t, err)
	require.Len(t, data, 2)

	byModel := make(map[string]*TokenUsageDataItem, len(data))
	for _, item := range data {
		byModel[item.ModelName] = item
	}
	require.Equal(t, 130, byModel["model-a"].InputTokens)
	require.Equal(t, 25, byModel["model-a"].OutputTokens)
	require.Equal(t, 50, byModel["model-a"].CachedTokens)
	require.Equal(t, 155, byModel["model-a"].TokenUsed)
	require.Equal(t, 50, byModel["model-b"].InputTokens)

	userData, err := GetTokenUsageData(createdAt, createdAt, "alice")
	require.NoError(t, err)
	require.Len(t, userData, 2)
	for _, item := range userData {
		require.Equal(t, "alice", item.Username)
		require.Equal(t, 1, item.UserID)
	}
}

func TestFlushTokenUsageDataCacheRetainsFailedBuckets(t *testing.T) {
	previousDB := DB
	t.Cleanup(func() { DB = previousDB })

	tokenUsageCacheLock.Lock()
	previousCache := tokenUsageCache
	tokenUsageCache = make(map[string]*TokenUsageData)
	tokenUsageCacheLock.Unlock()
	t.Cleanup(func() {
		tokenUsageCacheLock.Lock()
		tokenUsageCache = previousCache
		tokenUsageCacheLock.Unlock()
	})

	DB = newTokenUsageTestDB(t)
	LogTokenUsageData(TokenUsageDataLogParams{
		UserID: 3, Username: "alice", ModelName: "model-a", CreatedAt: 1_700_000_000,
		InputTokens: 10, OutputTokens: 5, CachedTokens: 2,
	})

	sqlDB, err := DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	require.Error(t, FlushTokenUsageDataCache())

	tokenUsageCacheLock.Lock()
	require.Len(t, tokenUsageCache, 1)
	tokenUsageCacheLock.Unlock()

	DB = newTokenUsageTestDB(t)
	require.NoError(t, FlushTokenUsageDataCache())

	var stored TokenUsageData
	require.NoError(t, DB.First(&stored).Error)
	require.Equal(t, 10, stored.InputTokens)
	require.Equal(t, 5, stored.OutputTokens)
	require.Equal(t, 2, stored.CachedTokens)
}

func TestGetConsumeLogsForTokenUsagePaginatesClickHouseZeroIDs(t *testing.T) {
	previousLogDB := LOG_DB
	previousLogDatabaseType := common.LogDatabaseType()
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogDatabaseType)
	})

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.Exec(`
		CREATE TABLE logs (
			id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			username TEXT NOT NULL,
			model_name TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			type INTEGER NOT NULL,
			prompt_tokens INTEGER NOT NULL,
			completion_tokens INTEGER NOT NULL,
			other TEXT NOT NULL,
			request_id TEXT NOT NULL
		)
	`).Error)
	for _, requestID := range []string{"request-a", "request-b", "request-c"} {
		require.NoError(t, db.Exec(
			`INSERT INTO logs (id, user_id, username, model_name, created_at, type, prompt_tokens, completion_tokens, other, request_id) VALUES (0, 1, 'alice', 'model-a', 1700000000, 2, 1, 1, '{}', ?)`,
			requestID,
		).Error)
	}

	LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)

	first, cursor, err := GetConsumeLogsForTokenUsage(1_699_999_000, 1_700_001_000, TokenUsageLogCursor{}, 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.Equal(t, 2, cursor.Offset)
	require.Zero(t, cursor.AfterID)

	second, cursor, err := GetConsumeLogsForTokenUsage(1_699_999_000, 1_700_001_000, cursor, 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	require.Equal(t, 3, cursor.Offset)
}
