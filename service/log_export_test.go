package service

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLogExportRequest(format string, columns ...LogExportColumn) *LogExportRequest {
	return &LogExportRequest{
		Category: "common", Format: format, Scope: "self", Columns: columns,
		Presentation: LogExportPresentation{
			Locale: "en", TimeZone: "UTC",
			Labels: map[string]string{
				"export_title": "Usage Logs Export", "category": "Category", "scope": "Scope",
				"generated_at": "Generated At", "row_count": "Row Count",
				"category:common": "Common", "scope:self": "Only Mine",
			},
		},
	}
}

func TestValidateLogExportRequestPermissionsAndColumns(t *testing.T) {
	valid := testLogExportRequest("csv", LogExportColumn{Key: "created_at", Label: "Time"})
	require.NoError(t, ValidateLogExportRequest(valid, common.RoleCommonUser))

	tests := []struct {
		name    string
		request *LogExportRequest
		role    int
		message string
	}{
		{"non-admin all scope", &LogExportRequest{Category: "common", Format: "csv", Scope: "all", Columns: []LogExportColumn{{Key: "created_at", Label: "Time"}}, Presentation: LogExportPresentation{Locale: "en"}}, common.RoleCommonUser, "administrator"},
		{"self admin column", &LogExportRequest{Category: "common", Format: "csv", Scope: "self", Columns: []LogExportColumn{{Key: "channel", Label: "Channel"}}, Presentation: LogExportPresentation{Locale: "en"}}, common.RoleAdminUser, "admin-only"},
		{"interactive drawing column", &LogExportRequest{Category: "drawing", Format: "json", Scope: "self", Columns: []LogExportColumn{{Key: "image_url", Label: "Image"}}, Presentation: LogExportPresentation{Locale: "en"}}, common.RoleCommonUser, "invalid export column"},
		{"duplicate column", &LogExportRequest{Category: "task", Format: "md", Scope: "self", Columns: []LogExportColumn{{Key: "status", Label: "Status"}, {Key: "status", Label: "Status"}}, Presentation: LogExportPresentation{Locale: "en"}}, common.RoleCommonUser, "invalid export column"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateLogExportRequest(test.request, test.role)
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.message)
		})
	}

	invalidTimeZone := testLogExportRequest("csv", LogExportColumn{Key: "created_at", Label: "Time"})
	invalidTimeZone.Presentation.TimeZone = "Not/A_Real_Zone"
	require.ErrorContains(t, ValidateLogExportRequest(invalidTimeZone, common.RoleCommonUser), "time zone")

	longFilter := testLogExportRequest("csv", LogExportColumn{Key: "created_at", Label: "Time"})
	longFilter.Filters.ModelName = strings.Repeat("x", 513)
	require.ErrorContains(t, ValidateLogExportRequest(longFilter, common.RoleCommonUser), "too long")

	admin := &LogExportRequest{Category: "common", Format: "json", Scope: "all", Columns: []LogExportColumn{{Key: "channel", Label: "Channel"}, {Key: "user", Label: "User"}}, Presentation: LogExportPresentation{Locale: "en"}}
	require.NoError(t, ValidateLogExportRequest(admin, common.RoleAdminUser))
}

func TestLogExportCSVContract(t *testing.T) {
	request := testLogExportRequest("csv",
		LogExportColumn{Key: "token_name", Label: "Token"},
		LogExportColumn{Key: "content", Label: "Details"},
	)
	var buffer bytes.Buffer
	output := &logExportOutput{request: request, writer: &buffer, firstJSON: true, generatedAt: time.Unix(100, 0).UTC()}
	require.NoError(t, output.begin())
	require.NoError(t, output.writeRow(logExportRow{Display: map[string]string{
		"token_name": "=SUM(A1:A2)",
		"content":    "quote \" and comma,\nUnicode 中文",
	}}))
	output.rowCount = 1
	require.NoError(t, output.finish())

	data := buffer.Bytes()
	require.True(t, bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	reader := csv.NewReader(bytes.NewReader(data[3:]))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.Equal(t, []string{"Token", "Details"}, records[0])
	assert.Equal(t, "'=SUM(A1:A2)", records[1][0])
	assert.Equal(t, "quote \" and comma,\nUnicode 中文", records[1][1])
}

func TestLogExportJSONAndMarkdownContracts(t *testing.T) {
	jsonRequest := testLogExportRequest("json", LogExportColumn{Key: "prompt_tokens", Label: "Tokens"})
	var jsonBuffer bytes.Buffer
	jsonOutput := &logExportOutput{request: jsonRequest, task: &model.LogExportTask{TotalRows: 1}, writer: &jsonBuffer, firstJSON: true, generatedAt: time.Unix(100, 0).UTC()}
	require.NoError(t, jsonOutput.begin())
	require.NoError(t, jsonOutput.writeRow(logExportRow{Raw: map[string]any{
		"prompt_tokens": map[string]any{"prompt": 3, "completion": 5, "cache_read": 2},
	}}))
	jsonOutput.rowCount = 1
	require.NoError(t, jsonOutput.finish())
	var document struct {
		Meta  map[string]any   `json:"meta"`
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, common.Unmarshal(jsonBuffer.Bytes(), &document))
	require.Len(t, document.Items, 1)
	assert.Equal(t, float64(1), document.Meta["row_count"])
	assert.Contains(t, document.Items[0], "prompt_tokens")
	assert.NotContains(t, document.Items[0], "Tokens")

	markdownRequest := testLogExportRequest("md", LogExportColumn{Key: "content", Label: "Details|Text"})
	var markdownBuffer bytes.Buffer
	markdownOutput := &logExportOutput{request: markdownRequest, task: &model.LogExportTask{TotalRows: 1}, writer: &markdownBuffer, generatedAt: time.Unix(100, 0).UTC()}
	require.NoError(t, markdownOutput.begin())
	require.NoError(t, markdownOutput.writeRow(logExportRow{Display: map[string]string{"content": "a|b\\c\nnext"}}))
	markdownOutput.rowCount = 1
	require.NoError(t, markdownOutput.finish())
	markdown := markdownBuffer.String()
	assert.Contains(t, markdown, "Details\\|Text")
	assert.Contains(t, markdown, "a\\|b\\\\c<br>next")
	assert.Contains(t, markdown, "- **Row Count:** 1")
}

func TestLogExportSensitiveValuesAreMaskedBeforeSerialization(t *testing.T) {
	request := testLogExportRequest("json",
		LogExportColumn{Key: "channel", Label: "Channel"},
		LogExportColumn{Key: "user", Label: "User"},
		LogExportColumn{Key: "token_name", Label: "Token"},
	)
	request.Scope = "all"
	request.SensitiveVisible = false
	row := buildCommonLogExportRow(&model.Log{
		UserId: 7, Username: "alice", ChannelId: 8, ChannelName: "secret-channel",
		TokenId: 9, TokenName: "secret-token", Group: "secret-group", CreatedAt: 100,
	}, request)
	data, err := common.Marshal(row.Raw)
	require.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "alice")
	assert.NotContains(t, text, "secret-channel")
	assert.NotContains(t, text, "secret-token")
	assert.NotContains(t, text, "secret-group")
	assert.Contains(t, text, "••••")

	taskRow := buildTaskLogExportRow(&model.Task{
		UserId: 7, Username: "alice", TaskID: "public-task", SubmitTime: 100,
		PrivateData: model.TaskPrivateData{Key: "private-key", UpstreamTaskID: "private-task"},
	}, request)
	taskData, err := common.Marshal(taskRow.Raw)
	require.NoError(t, err)
	assert.NotContains(t, string(taskData), "private-key")
	assert.NotContains(t, string(taskData), "private-task")
}

func TestLogExportChunkWriterRoundTripAndSizeBoundary(t *testing.T) {
	truncate(t)
	payload := bytes.Repeat([]byte("export-data-中文\n"), 90000)
	writer := &logExportChunkWriter{taskID: "logexp_chunk_roundtrip"}
	written, err := writer.Write(payload)
	require.NoError(t, err)
	assert.Equal(t, len(payload), written)
	require.NoError(t, writer.Close())
	assert.GreaterOrEqual(t, writer.sequence, 2)

	var restored bytes.Buffer
	require.NoError(t, StreamLogExportContent(&restored, writer.taskID))
	assert.Equal(t, payload, restored.Bytes())

	boundary := &logExportChunkWriter{taskID: "logexp_boundary", rawSize: logExportMaxBytes - 1}
	written, err = boundary.Write([]byte("x"))
	require.NoError(t, err)
	assert.Equal(t, 1, written)
	assert.Equal(t, logExportMaxBytes, boundary.rawSize)
	written, err = boundary.Write([]byte("x"))
	require.Error(t, err)
	assert.Zero(t, written)
	assert.Equal(t, logExportMaxBytes, boundary.rawSize)
}

func TestLogExportRowLimitBoundary(t *testing.T) {
	require.NoError(t, validateLogExportRowCount(logExportMaxRows))
	err := validateLogExportRowCount(logExportMaxRows + 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "100,000")
}

func TestLogExportFailureRemovesPartialChunks(t *testing.T) {
	truncate(t)
	task := &model.LogExportTask{
		TaskID: "logexp_partial_cleanup", UserID: 1, Category: "common", Format: "csv",
		Scope: "self", Status: model.LogExportStatusRunning, LockedBy: "runner-partial",
		LockedUntil: common.GetTimestamp() + 60,
	}
	require.NoError(t, model.DB.Create(task).Error)
	writer := &logExportChunkWriter{taskID: task.TaskID}
	_, err := writer.Write(bytes.Repeat([]byte("x"), logExportChunkRawSize))
	require.NoError(t, err)
	require.Equal(t, 1, writer.sequence)

	writer.rawSize = logExportMaxBytes
	_, limitErr := writer.Write([]byte("overflow"))
	require.Error(t, limitErr)
	failLogExportTask(task, "runner-partial", limitErr)

	var chunkCount int64
	require.NoError(t, model.DB.Model(&model.LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunkCount).Error)
	assert.Zero(t, chunkCount)
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(task).Error)
	assert.Equal(t, model.LogExportStatusFailed, task.Status)
	assert.Contains(t, task.Error, "100 MB")
}

func TestLogExportChunkStreamRejectsCorruptGzip(t *testing.T) {
	truncate(t)
	require.NoError(t, model.CreateLogExportChunk(&model.LogExportChunk{
		TaskID: "logexp_corrupt", Sequence: 0, Data: []byte("not-gzip"),
	}))
	err := StreamLogExportContent(io.Discard, "logexp_corrupt")
	require.Error(t, err)
	assert.False(t, errors.Is(err, io.EOF))
}

func TestLogExportAllScopeRechecksAdministratorRole(t *testing.T) {
	truncate(t)
	user := &model.User{
		Id: 9201, Username: "downgraded-admin", AffCode: "export-downgraded-admin",
		Role: common.RoleCommonUser, Status: common.UserStatusEnabled,
	}
	require.NoError(t, model.DB.Create(user).Error)
	request := testLogExportRequest("json", LogExportColumn{Key: "created_at", Label: "Time"})
	request.Scope = "all"
	request.SnapshotID = 0
	requestData, err := common.Marshal(request)
	require.NoError(t, err)
	task := &model.LogExportTask{
		TaskID: "logexp_role_recheck", UserID: user.Id, Category: "common", Format: "json",
		Scope: "all", Status: model.LogExportStatusRunning, Request: string(requestData),
		LockedBy: "runner-role", LockedUntil: common.GetTimestamp() + 60,
		SnapshotAt: time.Now().UnixMilli(),
	}
	require.NoError(t, model.DB.Create(task).Error)

	runLogExportTask(task, "runner-role")
	require.NoError(t, model.DB.Where("task_id = ?", task.TaskID).First(task).Error)
	assert.Equal(t, model.LogExportStatusFailed, task.Status)
	assert.Contains(t, task.Error, "administrator permission")
	assert.Zero(t, task.ChunkCount)
}

func TestEscapeCSVFormulaIgnoresOrdinaryNegativeContext(t *testing.T) {
	assert.Equal(t, "-1", escapeCSVFormula("-1"))
	assert.Equal(t, "'  @cmd", escapeCSVFormula("  @cmd"))
	assert.Equal(t, "plain-text", escapeCSVFormula("plain-text"))
	assert.Equal(t, "-12.50", escapeCSVFormula("-12.50"))
	assert.Equal(t, "'-1+2", escapeCSVFormula("-1+2"))
	assert.Equal(t, "line\\\\break", escapeMarkdown("line\\break"))
	assert.True(t, strings.HasPrefix(escapeCSVFormula("+SUM(A:A)"), "'"))
}
