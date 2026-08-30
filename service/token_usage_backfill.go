package service

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	tokenUsageBackfillDays  = 30
	tokenUsageBackfillBatch = 500
)

type TokenUsageBackfillPayload struct {
	StartTimestamp int64 `json:"start_timestamp"`
	EndTimestamp   int64 `json:"end_timestamp"`
}

type TokenUsageBackfillResult struct {
	StartTimestamp      int64 `json:"start_timestamp"`
	EndTimestamp        int64 `json:"end_timestamp"`
	ScannedLogs         int64 `json:"scanned_logs"`
	LogsWithCacheTokens int64 `json:"logs_with_cache_tokens"`
	WrittenBuckets      int64 `json:"written_buckets"`
	CachedTokens        int64 `json:"cached_tokens"`
}

func StartTokenUsageBackfillTask() (*model.SystemTask, error) {
	currentHour := common.GetTimestamp() / 3600 * 3600
	start := currentHour - int64((tokenUsageBackfillDays * 24 * time.Hour).Seconds())
	payload := TokenUsageBackfillPayload{StartTimestamp: start, EndTimestamp: currentHour - 1}
	task, _, err := EnqueueSystemTask(model.SystemTaskTypeTokenUsageBackfill, payload)
	return task, err
}

func RunTokenUsageBackfill(ctx context.Context, task *model.SystemTask, reporter func(processed, total int)) (TokenUsageBackfillResult, error) {
	payload := TokenUsageBackfillPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		return TokenUsageBackfillResult{}, err
	}
	if payload.StartTimestamp <= 0 || payload.EndTimestamp < payload.StartTimestamp {
		return TokenUsageBackfillResult{}, errors.New("invalid token usage backfill time range")
	}
	if err := model.FlushTokenUsageDataCache(); err != nil {
		return TokenUsageBackfillResult{}, err
	}

	total, err := model.CountConsumeLogsForTokenUsage(payload.StartTimestamp, payload.EndTimestamp)
	if err != nil {
		return TokenUsageBackfillResult{}, err
	}
	result := TokenUsageBackfillResult{
		StartTimestamp: payload.StartTimestamp,
		EndTimestamp:   payload.EndTimestamp,
	}
	buckets := make(map[string]*model.TokenUsageData)
	cursor := model.TokenUsageLogCursor{}
	processed := int64(0)
	reporter(0, safeProgressTotal(total))

	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		logs, nextCursor, err := model.GetConsumeLogsForTokenUsage(payload.StartTimestamp, payload.EndTimestamp, cursor, tokenUsageBackfillBatch)
		if err != nil {
			return result, err
		}
		cursor = nextCursor
		if len(logs) == 0 {
			break
		}
		for _, log := range logs {
			processed++
			result.ScannedLogs++
			cachedTokens, hasCache := legacyCachedTokens(log.Other)
			if hasCache && cachedTokens > 0 {
				result.LogsWithCacheTokens++
				result.CachedTokens += int64(cachedTokens)
			}
			inputTokens := maxNonNegative(log.PromptTokens)
			outputTokens := maxNonNegative(log.CompletionTokens)
			if inputTokens == 0 && outputTokens == 0 && cachedTokens == 0 {
				continue
			}
			data := &model.TokenUsageData{
				UserID: log.UserID, Username: log.Username, ModelName: log.ModelName,
				CreatedAt:   log.CreatedAt - log.CreatedAt%3600,
				InputTokens: inputTokens, OutputTokens: outputTokens, CachedTokens: cachedTokens,
			}
			key := fmt.Sprintf("%d\x00%s\x00%s\x00%d", data.UserID, data.Username, data.ModelName, data.CreatedAt)
			if existing := buckets[key]; existing != nil {
				existing.InputTokens += data.InputTokens
				existing.OutputTokens += data.OutputTokens
				existing.CachedTokens += data.CachedTokens
			} else {
				buckets[key] = data
			}
		}
		reporter(int(processed), safeProgressTotal(total))
		if len(logs) < tokenUsageBackfillBatch {
			break
		}
	}

	for _, data := range buckets {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := model.SetTokenUsageData(data); err != nil {
			return result, err
		}
		result.WrittenBuckets++
	}
	reporter(safeInt(processed), safeProgressTotal(total))
	return result, nil
}

func legacyCachedTokens(raw string) (int, bool) {
	values, err := common.StrToMap(raw)
	if err != nil || values == nil {
		return 0, false
	}
	for _, key := range []string{"cache_tokens", "cached_tokens", "prompt_cache_hit_tokens"} {
		if value, ok := values[key]; ok {
			if parsed, valid := numberToInt(value); valid {
				return parsed, true
			}
		}
	}
	return 0, false
}

func numberToInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, v >= 0
	case int64:
		return safeInt64ToInt(v), v >= 0
	case float64:
		if v < 0 || v > float64(math.MaxInt) || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return int(v), true
	case string:
		parsed, err := strconv.ParseInt(v, 10, 64)
		if err != nil || parsed < 0 {
			return 0, false
		}
		return safeInt64ToInt(parsed), true
	default:
		return 0, false
	}
}

func safeInt64ToInt(value int64) int {
	if value > int64(math.MaxInt) {
		return math.MaxInt
	}
	return int(value)
}

func maxNonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

func safeProgressTotal(total int64) int {
	if total <= 0 {
		return 1
	}
	return safeInt64ToInt(total)
}

func safeInt(value int64) int {
	return safeInt64ToInt(value)
}
