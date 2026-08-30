package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newTokenUsageBackfillTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.TokenUsageData{}, &model.Log{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	return db
}

func TestLegacyCachedTokensParsesSupportedFieldsAndRejectsInvalidValues(t *testing.T) {
	for _, test := range []struct {
		name    string
		other   string
		want    int
		present bool
	}{
		{name: "cache tokens", other: `{"cache_tokens":12}`, want: 12, present: true},
		{name: "cached tokens", other: `{"cached_tokens":7}`, want: 7, present: true},
		{name: "prompt cache hit", other: `{"prompt_cache_hit_tokens":"9"}`, want: 9, present: true},
		{name: "missing", other: `{"model_ratio":1}`, want: 0, present: false},
		{name: "negative", other: `{"cache_tokens":-1}`, want: 0, present: false},
		{name: "invalid json", other: `{`, want: 0, present: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, present := legacyCachedTokens(test.other)
			assert.Equal(t, test.want, got)
			assert.Equal(t, test.present, present)
		})
	}
}

func TestRunTokenUsageBackfillIsIdempotent(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })
	db := newTokenUsageBackfillTestDB(t)
	model.DB, model.LOG_DB = db, db

	start := int64(1_700_000_000)
	require.NoError(t, db.Create(&model.Log{
		UserId: 1, Username: "alice", ModelName: "model-a", Type: model.LogTypeConsume,
		CreatedAt: start + 100, PromptTokens: 100, CompletionTokens: 25,
		Other: `{"cache_tokens":40}`,
	}).Error)
	require.NoError(t, db.Create(&model.Log{
		UserId: 1, Username: "alice", ModelName: "model-a", Type: model.LogTypeConsume,
		CreatedAt: start + 200, PromptTokens: 50, CompletionTokens: 10,
		Other: `{"model_ratio":1}`,
	}).Error)

	task, err := model.CreateSystemTask(model.SystemTaskTypeTokenUsageBackfill,
		TokenUsageBackfillPayload{StartTimestamp: start, EndTimestamp: start + 3600}, nil)
	require.NoError(t, err)
	reporter := func(processed, total int) {}
	first, err := RunTokenUsageBackfill(context.Background(), task, reporter)
	require.NoError(t, err)
	require.Equal(t, int64(2), first.ScannedLogs)
	require.Equal(t, int64(1), first.LogsWithCacheTokens)
	require.Equal(t, int64(40), first.CachedTokens)

	second, err := RunTokenUsageBackfill(context.Background(), task, reporter)
	require.NoError(t, err)
	require.Equal(t, first.ScannedLogs, second.ScannedLogs)

	var stored model.TokenUsageData
	require.NoError(t, db.First(&stored).Error)
	require.Equal(t, 150, stored.InputTokens)
	require.Equal(t, 35, stored.OutputTokens)
	require.Equal(t, 40, stored.CachedTokens)
}

func TestStartTokenUsageBackfillTaskUsesCompletedHoursOnly(t *testing.T) {
	previousDB := model.DB
	t.Cleanup(func() { model.DB = previousDB })
	model.DB = newTokenUsageBackfillTestDB(t)

	task, err := StartTokenUsageBackfillTask()
	require.NoError(t, err)

	payload := TokenUsageBackfillPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	require.Equal(t, int64(0), payload.StartTimestamp%3600)
	require.Equal(t, int64(3599), payload.EndTimestamp%3600)
	require.Equal(t, int64(tokenUsageBackfillDays*24*3600), payload.EndTimestamp-payload.StartTimestamp+1)
}
