package service

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newTokenUsageBackfillTestDB(t *testing.T, dsn string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	if dsn != "" {
		if strings.HasPrefix(dsn, "postgres://") {
			dialector, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
		} else {
			dialector, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
		}
	}
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(dbType, dbType)
	t.Cleanup(func() { common.SetDatabaseTypes(oldMain, oldLog) })
	db, err := gorm.Open(dialector, &gorm.Config{})
	require.NoError(t, err)
	// A private in-memory database is per-connection. Pin the pool to one
	// connection so every query in a test observes the same database.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	if dbType != common.DatabaseTypeSQLite {
		// These DSNs must point to isolated test databases, just like the performance fixture.
		require.NoError(t, db.Migrator().DropTable(&model.TokenUsageData{}, &model.Log{}, &model.SystemTask{}, &model.SystemTaskLock{}))
	}
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
	for _, dialect := range []struct{ name, env string }{
		{"sqlite", ""}, {"mysql", "TEST_TOKEN_USAGE_MYSQL_DSN"}, {"postgres", "TEST_TOKEN_USAGE_POSTGRES_DSN"},
	} {
		t.Run(dialect.name, func(t *testing.T) {
			dsn := os.Getenv(dialect.env)
			if dialect.env != "" && dsn == "" {
				t.Skip("isolated test database DSN is not configured")
			}
			previousDB, previousLogDB := model.DB, model.LOG_DB
			t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })
			db := newTokenUsageBackfillTestDB(t, dsn)
			model.DB, model.LOG_DB = db, db

			start := int64(1_700_000_000 / 3600 * 3600)
			for _, entry := range []model.Log{
				{PromptTokens: 100, CompletionTokens: 25, Other: `{"cache_tokens":40}`},
				{PromptTokens: 50, CompletionTokens: 10, Other: `{"usage_semantic":"anthropic","cache_tokens":200,"cache_creation_tokens":20,"cache_creation_tokens_5m":10,"cache_creation_tokens_1h":10}`},
				{PromptTokens: 100, CompletionTokens: 5, Other: `{"usage_semantic":"openai","claude":true,"cache_tokens":80,"cache_creation_tokens":10}`},
				{PromptTokens: 10, Other: `{"claude":true,"cache_tokens":20,"cache_creation_tokens":3}`},
				{PromptTokens: 5, CompletionTokens: 2, Other: `{"usage_semantic":"anthropic","cache_tokens":30,"cache_creation_tokens_5m":2,"cache_creation_tokens_1h":3}`},
			} {
				entry.UserId, entry.Username, entry.ModelName = 1, "alice", "model-a"
				entry.Type, entry.CreatedAt = model.LogTypeConsume, start+100
				require.NoError(t, db.Create(&entry).Error)
			}

			task, err := model.CreateSystemTask(model.SystemTaskTypeTokenUsageBackfill,
				TokenUsageBackfillPayload{StartTimestamp: start, EndTimestamp: start + 3599}, nil)
			require.NoError(t, err)
			reporter := func(processed, total int) {}
			first, err := RunTokenUsageBackfill(context.Background(), task, reporter)
			require.NoError(t, err)
			require.Equal(t, int64(5), first.ScannedLogs)
			require.Equal(t, int64(5), first.LogsWithCacheTokens)
			require.Equal(t, int64(370), first.CachedTokens)

			second, err := RunTokenUsageBackfill(context.Background(), task, reporter)
			require.NoError(t, err)
			require.Equal(t, first.ScannedLogs, second.ScannedLogs)

			var stored model.TokenUsageData
			// RunTokenUsageBackfill flushes the process-wide pending cache first, so the
			// table may contain unrelated buckets from earlier tests. Query this test's
			// own bucket instead of the first row.
			require.NoError(t, db.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
				1, "alice", "model-a", start).First(&stored).Error)
			require.Equal(t, 543, stored.InputTokens)
			require.Equal(t, 42, stored.OutputTokens)
			require.Equal(t, 370, stored.CachedTokens)
		})
	}
}

func TestRunTokenUsageBackfillAggregatesAcrossBatches(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	t.Cleanup(func() { model.DB, model.LOG_DB = previousDB, previousLogDB })
	db := newTokenUsageBackfillTestDB(t, "")
	model.DB, model.LOG_DB = db, db

	start := int64(1_700_000_000 / 3600 * 3600)
	for range tokenUsageBackfillBatch + 1 {
		require.NoError(t, db.Create(&model.Log{
			UserId: 1, Username: "alice", ModelName: "model-a", Type: model.LogTypeConsume,
			CreatedAt: start + 100, PromptTokens: 1, CompletionTokens: 2,
			Other: `{"cache_tokens":1}`,
		}).Error)
	}

	task, err := model.CreateSystemTask(model.SystemTaskTypeTokenUsageBackfill,
		TokenUsageBackfillPayload{StartTimestamp: start, EndTimestamp: start + 3599}, nil)
	require.NoError(t, err)

	result, err := RunTokenUsageBackfill(context.Background(), task, func(processed, total int) {})
	require.NoError(t, err)
	require.Equal(t, int64(tokenUsageBackfillBatch+1), result.ScannedLogs)

	var stored model.TokenUsageData
	require.NoError(t, db.Where("user_id = ? AND username = ? AND model_name = ? AND created_at = ?",
		1, "alice", "model-a", start).First(&stored).Error)
	require.Equal(t, tokenUsageBackfillBatch+1, stored.InputTokens)
	require.Equal(t, (tokenUsageBackfillBatch+1)*2, stored.OutputTokens)
	require.Equal(t, tokenUsageBackfillBatch+1, stored.CachedTokens)
}

func TestStartTokenUsageBackfillTaskUsesCompletedHoursOnly(t *testing.T) {
	previousDB := model.DB
	t.Cleanup(func() { model.DB = previousDB })
	model.DB = newTokenUsageBackfillTestDB(t, "")

	task, err := StartTokenUsageBackfillTask()
	require.NoError(t, err)

	payload := TokenUsageBackfillPayload{}
	require.NoError(t, task.DecodePayload(&payload))
	require.Equal(t, int64(0), payload.StartTimestamp%3600)
	require.Equal(t, int64(3599), payload.EndTimestamp%3600)
	require.Equal(t, int64(tokenUsageBackfillDays*24*3600), payload.EndTimestamp-payload.StartTimestamp+1)
}
