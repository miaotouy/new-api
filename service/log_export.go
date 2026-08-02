package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

const (
	logExportBatchSize               = 1000
	logExportChunkRawSize            = 1024 * 1024
	logExportMaxRows                 = 100000
	logExportMaxBytes          int64 = 100 * 1024 * 1024
	logExportLeaseDuration           = 60 * time.Second
	logExportPollInterval            = 5 * time.Second
	logExportCleanupInterval         = 15 * time.Minute
	logExportWorkerCount             = 2
	logExportRecoveryBatchSize       = 100
)

var (
	logExportWorkerOnce sync.Once
	logExportWakeup     = make(chan struct{}, logExportWorkerCount)
)

type LogExportColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type LogExportFilters struct {
	Type              *int   `json:"type,omitempty"`
	Username          string `json:"username,omitempty"`
	TokenName         string `json:"token_name,omitempty"`
	ModelName         string `json:"model_name,omitempty"`
	StartTimestamp    *int64 `json:"start_timestamp,omitempty"`
	EndTimestamp      *int64 `json:"end_timestamp,omitempty"`
	Channel           *int   `json:"channel,omitempty"`
	Group             string `json:"group,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	UpstreamRequestID string `json:"upstream_request_id,omitempty"`
	ChannelID         string `json:"channel_id,omitempty"`
	MJID              string `json:"mj_id,omitempty"`
	TaskID            string `json:"task_id,omitempty"`
}

type LogExportPresentation struct {
	Locale           string            `json:"locale"`
	TimeZone         string            `json:"time_zone"`
	Labels           map[string]string `json:"labels,omitempty"`
	QuotaPerUnit     float64           `json:"quota_per_unit,omitempty"`
	CurrencySymbol   string            `json:"currency_symbol,omitempty"`
	CurrencyRate     float64           `json:"currency_rate,omitempty"`
	QuotaDisplayMode string            `json:"quota_display_mode,omitempty"`
}

type LogExportRequest struct {
	SnapshotID       int64                 `json:"snapshot_id,omitempty"`
	Category         string                `json:"category"`
	Format           string                `json:"format"`
	Scope            string                `json:"scope"`
	Filters          LogExportFilters      `json:"filters"`
	Columns          []LogExportColumn     `json:"columns"`
	SensitiveVisible bool                  `json:"sensitive_visible"`
	Presentation     LogExportPresentation `json:"presentation"`
}

var logExportColumns = map[string]map[string]bool{
	"common": {
		"created_at": true, "channel": true, "user": true, "token_name": true,
		"model_name": true, "is_stream": true, "prompt_tokens": true,
		"quota": true, "use_time": true, "content": true,
	},
	"drawing": {
		"submit_time": true, "channel_id": true, "action": true, "mj_id": true,
		"duration": true, "code": true, "progress": true, "prompt": true,
	},
	"task": {
		"submit_time": true, "channel_id": true, "user": true, "task_id": true,
		"duration": true, "status": true, "progress": true, "fail_reason": true,
	},
}

var logExportAdminColumns = map[string]map[string]bool{
	"common":  {"channel": true, "user": true},
	"drawing": {"channel_id": true, "code": true},
	"task":    {"channel_id": true, "user": true},
}

func ValidateLogExportRequest(request *LogExportRequest, role int) error {
	if request == nil {
		return errors.New("export request is required")
	}
	allowedColumns, ok := logExportColumns[request.Category]
	if !ok {
		return errors.New("unsupported log category")
	}
	if request.Format != "csv" && request.Format != "json" && request.Format != "md" {
		return errors.New("unsupported export format")
	}
	if request.Scope != "self" && request.Scope != "all" {
		return errors.New("unsupported export scope")
	}
	if request.Scope == "all" && role < common.RoleAdminUser {
		return errors.New("administrator permission is required")
	}
	if len(request.Columns) == 0 {
		return errors.New("at least one export column is required")
	}
	if len(request.Columns) > len(allowedColumns) {
		return errors.New("too many export columns")
	}
	seen := make(map[string]bool, len(request.Columns))
	for _, column := range request.Columns {
		if !allowedColumns[column.Key] || seen[column.Key] {
			return fmt.Errorf("invalid export column: %s", column.Key)
		}
		if request.Scope != "all" && logExportAdminColumns[request.Category][column.Key] {
			return fmt.Errorf("admin-only export column: %s", column.Key)
		}
		if strings.TrimSpace(column.Label) == "" || len(column.Label) > 128 {
			return fmt.Errorf("invalid export column label: %s", column.Key)
		}
		seen[column.Key] = true
	}
	if request.Presentation.Locale == "" || len(request.Presentation.TimeZone) > 128 || len(request.Presentation.Locale) > 32 {
		return errors.New("invalid export presentation")
	}
	if request.Presentation.TimeZone != "" {
		if _, err := time.LoadLocation(request.Presentation.TimeZone); err != nil {
			return errors.New("invalid export time zone")
		}
	}
	filterValues := []string{
		request.Filters.Username, request.Filters.TokenName, request.Filters.ModelName,
		request.Filters.Group, request.Filters.RequestID, request.Filters.UpstreamRequestID,
		request.Filters.ChannelID, request.Filters.MJID, request.Filters.TaskID,
	}
	for _, value := range filterValues {
		if len(value) > 512 {
			return errors.New("export filter is too long")
		}
	}
	if request.Filters.StartTimestamp != nil && *request.Filters.StartTimestamp < 0 {
		return errors.New("invalid export start time")
	}
	if request.Filters.EndTimestamp != nil && *request.Filters.EndTimestamp < 0 {
		return errors.New("invalid export end time")
	}
	if request.Filters.Channel != nil && *request.Filters.Channel < 0 {
		return errors.New("invalid export channel")
	}
	if request.Presentation.QuotaPerUnit < 0 || request.Presentation.CurrencyRate < 0 || len(request.Presentation.CurrencySymbol) > 32 {
		return errors.New("invalid export presentation")
	}
	if len(request.Presentation.Labels) > 128 {
		return errors.New("too many export labels")
	}
	for key, value := range request.Presentation.Labels {
		if len(key) > 128 || len(value) > 256 {
			return errors.New("invalid export label")
		}
	}
	return nil
}

func WakeLogExportWorker() {
	for i := 0; i < logExportWorkerCount; i++ {
		select {
		case logExportWakeup <- struct{}{}:
		default:
		}
	}
}

func StartLogExportWorker() {
	logExportWorkerOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		for i := 0; i < logExportWorkerCount; i++ {
			runnerID := fmt.Sprintf("%s-log-export-%d-%s", common.NodeName, i, common.GetRandomString(6))
			gopool.Go(func() {
				ticker := time.NewTicker(logExportPollInterval)
				defer ticker.Stop()
				for {
					runLogExportPass(runnerID)
					select {
					case <-ticker.C:
					case <-logExportWakeup:
					}
				}
			})
		}
		gopool.Go(func() {
			ticker := time.NewTicker(logExportCleanupInterval)
			defer ticker.Stop()
			for {
				if _, err := model.CleanupExpiredLogExportTasks(common.GetTimestamp(), 100); err != nil {
					logger.LogWarn(context.Background(), fmt.Sprintf("log export cleanup failed: %v", err))
				}
				<-ticker.C
			}
		})
	})
}

func runLogExportPass(runnerID string) {
	now := common.GetTimestamp()
	if err := model.RecoverStaleLogExportTasks(now, logExportRecoveryBatchSize); err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("log export recovery failed: %v", err))
		return
	}
	for {
		task, err := model.ClaimNextLogExportTask(runnerID, now+int64(logExportLeaseDuration.Seconds()))
		if err != nil {
			logger.LogWarn(context.Background(), fmt.Sprintf("log export claim failed: %v", err))
			return
		}
		if task == nil {
			return
		}
		runLogExportTask(task, runnerID)
		now = common.GetTimestamp()
	}
}

func runLogExportTask(task *model.LogExportTask, runnerID string) {
	startedAt := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leaseDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(logExportLeaseDuration / 3)
		defer ticker.Stop()
		defer close(leaseDone)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := model.RenewLogExportLease(task.TaskID, runnerID, common.GetTimestamp()+int64(logExportLeaseDuration.Seconds())); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	var request LogExportRequest
	if err := common.UnmarshalJsonStr(task.Request, &request); err != nil {
		failLogExportTask(task, runnerID, err)
		cancel()
		<-leaseDone
		return
	}
	if request.Scope == "all" {
		user, err := model.GetUserById(task.UserID, false)
		if err != nil || user.Role < common.RoleAdminUser {
			failLogExportTask(task, runnerID, errors.New("administrator permission is no longer available"))
			cancel()
			<-leaseDone
			return
		}
	}

	writer := &logExportChunkWriter{taskID: task.TaskID}
	rows, fileName, contentType, err := generateLogExport(ctx, task, runnerID, &request, writer)
	if err == nil {
		err = writer.Close()
	}
	if err != nil {
		failLogExportTask(task, runnerID, err)
		cancel()
		<-leaseDone
		return
	}
	if err := model.FinishLogExportTask(task.TaskID, runnerID, fileName, contentType, rows, writer.rawSize, writer.storedSize, writer.sequence); err != nil {
		failLogExportTask(task, runnerID, err)
		cancel()
		<-leaseDone
		return
	}
	cancel()
	<-leaseDone
	logger.LogInfo(context.Background(), fmt.Sprintf("log export succeeded: task_id=%s user_id=%d category=%s rows=%d size=%d duration=%s", task.TaskID, task.UserID, task.Category, rows, writer.rawSize, time.Since(startedAt)))
}

func failLogExportTask(task *model.LogExportTask, runnerID string, taskErr error) {
	transitioned, err := model.FailLogExportTask(task.TaskID, runnerID, taskErr)
	if err != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("log export failure update failed: task_id=%s user_id=%d category=%s err=%v", task.TaskID, task.UserID, task.Category, err))
		return
	}
	if transitioned {
		logger.LogWarn(context.Background(), fmt.Sprintf("log export failed: task_id=%s user_id=%d category=%s err=%v", task.TaskID, task.UserID, task.Category, taskErr))
	}
}

type logExportChunkWriter struct {
	taskID     string
	buffer     bytes.Buffer
	sequence   int
	rawSize    int64
	storedSize int64
}

func (writer *logExportChunkWriter) Write(data []byte) (int, error) {
	if writer.rawSize+int64(len(data)) > logExportMaxBytes {
		return 0, errors.New("export exceeds the 100 MB size limit")
	}
	written := 0
	for len(data) > 0 {
		remaining := logExportChunkRawSize - writer.buffer.Len()
		if remaining > len(data) {
			remaining = len(data)
		}
		n, err := writer.buffer.Write(data[:remaining])
		if err != nil {
			return written, err
		}
		writer.rawSize += int64(n)
		written += n
		data = data[n:]
		if writer.buffer.Len() >= logExportChunkRawSize {
			if err := writer.flush(); err != nil {
				return written, err
			}
		}
	}
	return written, nil
}

func (writer *logExportChunkWriter) Close() error {
	return writer.flush()
}

func (writer *logExportChunkWriter) flush() error {
	if writer.buffer.Len() == 0 {
		return nil
	}
	rawSize := writer.buffer.Len()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if _, err := io.Copy(gzipWriter, bytes.NewReader(writer.buffer.Bytes())); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	chunk := &model.LogExportChunk{
		TaskID:     writer.taskID,
		Sequence:   writer.sequence,
		Data:       append([]byte(nil), compressed.Bytes()...),
		StoredSize: int64(compressed.Len()),
		RawSize:    int64(rawSize),
	}
	if err := model.CreateLogExportChunk(chunk); err != nil {
		return err
	}
	writer.sequence++
	writer.storedSize += int64(compressed.Len())
	writer.buffer.Reset()
	return nil
}

func StreamLogExportContent(destination io.Writer, taskID string) error {
	afterSequence := -1
	for {
		chunks, err := model.ListLogExportChunks(taskID, afterSequence, 16)
		if err != nil {
			return err
		}
		if len(chunks) == 0 {
			return nil
		}
		for _, chunk := range chunks {
			reader, err := gzip.NewReader(bytes.NewReader(chunk.Data))
			if err != nil {
				return err
			}
			if _, err := io.Copy(destination, reader); err != nil {
				reader.Close()
				return err
			}
			if err := reader.Close(); err != nil {
				return err
			}
			afterSequence = chunk.Sequence
		}
	}
}

type logExportRow struct {
	Raw     map[string]any
	Display map[string]string
}

type logExportOutput struct {
	request     *LogExportRequest
	task        *model.LogExportTask
	writer      io.Writer
	csvWriter   *csv.Writer
	rowCount    int
	firstJSON   bool
	location    *time.Location
	generatedAt time.Time
}

func validateLogExportRowCount(totalRows int64) error {
	if totalRows > logExportMaxRows {
		return errors.New("export exceeds the 100,000 row limit")
	}
	return nil
}

func generateLogExport(ctx context.Context, task *model.LogExportTask, runnerID string, request *LogExportRequest, writer io.Writer) (int, string, string, error) {
	location := time.UTC
	if request.Presentation.TimeZone != "" {
		if candidate, err := time.LoadLocation(request.Presentation.TimeZone); err == nil {
			location = candidate
		}
	}
	generatedAt := time.UnixMilli(task.SnapshotAt).In(location)
	totalRows, err := countLogExportRows(task, request)
	if err != nil {
		return 0, "", "", err
	}
	if err := validateLogExportRowCount(totalRows); err != nil {
		return 0, "", "", err
	}
	task.TotalRows = int(totalRows)
	if err := model.UpdateLogExportProgress(task.TaskID, runnerID, 0, task.TotalRows, 0); err != nil {
		return 0, "", "", err
	}
	output := &logExportOutput{
		request: request, task: task, writer: writer, firstJSON: true,
		location: location, generatedAt: generatedAt,
	}
	if err := output.begin(); err != nil {
		return 0, "", "", err
	}

	consume := func(row logExportRow) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if output.rowCount >= logExportMaxRows {
			return errors.New("export exceeds the 100,000 row limit")
		}
		if err := output.writeRow(row); err != nil {
			return err
		}
		output.rowCount++
		if output.rowCount%logExportBatchSize == 0 {
			progress := 0
			if task.TotalRows > 0 {
				progress = int(math.Min(99, float64(output.rowCount)*100/float64(task.TotalRows)))
			}
			if err := model.UpdateLogExportProgress(task.TaskID, runnerID, output.rowCount, task.TotalRows, progress); err != nil {
				return err
			}
		}
		return nil
	}

	if err := iterateLogExportRows(ctx, task, request, consume); err != nil {
		return 0, "", "", err
	}
	if err := output.finish(); err != nil {
		return 0, "", "", err
	}
	extension := request.Format
	contentType := "text/plain; charset=utf-8"
	switch request.Format {
	case "csv":
		contentType = "text/csv; charset=utf-8"
	case "json":
		contentType = "application/json; charset=utf-8"
	case "md":
		contentType = "text/markdown; charset=utf-8"
	}
	fileName := fmt.Sprintf("usage-logs-%s-%s.%s", request.Category, generatedAt.Format("20060102-150405"), extension)
	return output.rowCount, fileName, contentType, nil
}

func (output *logExportOutput) begin() error {
	switch output.request.Format {
	case "csv":
		if _, err := output.writer.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
			return err
		}
		output.csvWriter = csv.NewWriter(output.writer)
		headers := make([]string, len(output.request.Columns))
		for i, column := range output.request.Columns {
			headers[i] = escapeCSVFormula(column.Label)
		}
		return output.csvWriter.Write(headers)
	case "json":
		meta := map[string]any{
			"category":    output.request.Category,
			"scope":       output.request.Scope,
			"exported_at": output.generatedAt.Format(time.RFC3339),
			"row_count":   output.task.TotalRows,
		}
		data, err := common.Marshal(meta)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(output.writer, `{"meta":`); err != nil {
			return err
		}
		if _, err := output.writer.Write(data); err != nil {
			return err
		}
		_, err = io.WriteString(output.writer, `,"items":[`)
		return err
	case "md":
		labels := output.request.Presentation.Labels
		title := labelOr(labels, "export_title", "Usage Logs Export")
		categoryLabel := labelOr(labels, "category", "Category")
		scopeLabel := labelOr(labels, "scope", "Scope")
		generatedLabel := labelOr(labels, "generated_at", "Generated At")
		rowCountLabel := labelOr(labels, "row_count", "Row Count")
		if _, err := fmt.Fprintf(output.writer, "# %s\n\n- **%s:** %s\n- **%s:** %s\n- **%s:** %s\n- **%s:** %d\n\n", title, categoryLabel, labelOr(labels, "category:"+output.request.Category, output.request.Category), scopeLabel, labelOr(labels, "scope:"+output.request.Scope, output.request.Scope), generatedLabel, output.generatedAt.Format("2006-01-02 15:04:05 MST"), rowCountLabel, output.task.TotalRows); err != nil {
			return err
		}
		headers := make([]string, len(output.request.Columns))
		separator := make([]string, len(output.request.Columns))
		for i, column := range output.request.Columns {
			headers[i] = escapeMarkdown(column.Label)
			separator[i] = "---"
		}
		if _, err := fmt.Fprintf(output.writer, "| %s |\n| %s |\n", strings.Join(headers, " | "), strings.Join(separator, " | ")); err != nil {
			return err
		}
	}
	return nil
}

func (output *logExportOutput) writeRow(row logExportRow) error {
	switch output.request.Format {
	case "csv":
		record := make([]string, len(output.request.Columns))
		for i, column := range output.request.Columns {
			record[i] = escapeCSVFormula(row.Display[column.Key])
		}
		if err := output.csvWriter.Write(record); err != nil {
			return err
		}
		output.csvWriter.Flush()
		return output.csvWriter.Error()
	case "json":
		item := make(map[string]any, len(output.request.Columns))
		for _, column := range output.request.Columns {
			item[column.Key] = row.Raw[column.Key]
		}
		data, err := common.Marshal(item)
		if err != nil {
			return err
		}
		if !output.firstJSON {
			if _, err := io.WriteString(output.writer, ","); err != nil {
				return err
			}
		}
		output.firstJSON = false
		_, err = output.writer.Write(data)
		return err
	case "md":
		values := make([]string, len(output.request.Columns))
		for i, column := range output.request.Columns {
			values[i] = escapeMarkdown(row.Display[column.Key])
		}
		_, err := fmt.Fprintf(output.writer, "| %s |\n", strings.Join(values, " | "))
		return err
	}
	return nil
}

func (output *logExportOutput) finish() error {
	switch output.request.Format {
	case "csv":
		output.csvWriter.Flush()
		return output.csvWriter.Error()
	case "json":
		_, err := io.WriteString(output.writer, "]}")
		return err
	}
	return nil
}

func commonLogExportQuery(task *model.LogExportTask, request *LogExportRequest) model.LogQueryParams {
	filters := request.Filters
	logType := 0
	if filters.Type != nil {
		logType = *filters.Type
	}
	startTimestamp := int64(0)
	if filters.StartTimestamp != nil {
		startTimestamp = *filters.StartTimestamp
	}
	endTimestamp := task.SnapshotAt / 1000
	if filters.EndTimestamp != nil && *filters.EndTimestamp < endTimestamp {
		endTimestamp = *filters.EndTimestamp
	}
	channel := 0
	if filters.Channel != nil {
		channel = *filters.Channel
	}
	params := model.LogQueryParams{
		MaxID:   int(request.SnapshotID),
		LogType: logType, StartTimestamp: startTimestamp, EndTimestamp: endTimestamp,
		ModelName: filters.ModelName, Username: filters.Username, TokenName: filters.TokenName,
		ChannelID: channel, Group: filters.Group, RequestID: filters.RequestID,
		UpstreamRequestID: filters.UpstreamRequestID,
	}
	if request.Scope != "all" {
		params.UserID = &task.UserID
	}
	return params
}

func drawingLogExportQuery(task *model.LogExportTask, request *LogExportRequest) (*int, model.TaskQueryParams) {
	filters := request.Filters
	endTimestamp := task.SnapshotAt
	if filters.EndTimestamp != nil && *filters.EndTimestamp < endTimestamp {
		endTimestamp = *filters.EndTimestamp
	}
	params := model.TaskQueryParams{
		MaxID:     int(request.SnapshotID),
		ChannelID: filters.ChannelID, MjID: filters.MJID,
		EndTimestamp: strconv.FormatInt(endTimestamp, 10),
	}
	if filters.StartTimestamp != nil {
		params.StartTimestamp = strconv.FormatInt(*filters.StartTimestamp, 10)
	}
	if request.Scope != "all" {
		params.ChannelID = ""
		return &task.UserID, params
	}
	return nil, params
}

func taskLogExportQuery(task *model.LogExportTask, request *LogExportRequest) (*int, model.SyncTaskQueryParams) {
	filters := request.Filters
	endTimestamp := task.SnapshotAt / 1000
	if filters.EndTimestamp != nil && *filters.EndTimestamp < endTimestamp {
		endTimestamp = *filters.EndTimestamp
	}
	params := model.SyncTaskQueryParams{
		MaxID:  request.SnapshotID,
		TaskID: filters.TaskID, EndTimestamp: endTimestamp, ChannelID: filters.ChannelID,
	}
	if filters.StartTimestamp != nil {
		params.StartTimestamp = *filters.StartTimestamp
	}
	if request.Scope != "all" {
		params.ChannelID = ""
		return &task.UserID, params
	}
	return nil, params
}

func countLogExportRows(task *model.LogExportTask, request *LogExportRequest) (int64, error) {
	switch request.Category {
	case "common":
		return model.CountLogs(commonLogExportQuery(task, request))
	case "drawing":
		userID, params := drawingLogExportQuery(task, request)
		return model.CountMidjourneyTasks(userID, params)
	case "task":
		userID, params := taskLogExportQuery(task, request)
		return model.CountSyncTasks(userID, params)
	default:
		return 0, errors.New("unsupported log category")
	}
}

func iterateLogExportRows(ctx context.Context, task *model.LogExportTask, request *LogExportRequest, consume func(logExportRow) error) error {
	switch request.Category {
	case "common":
		return iterateCommonLogExport(ctx, task, request, consume)
	case "drawing":
		return iterateDrawingLogExport(ctx, task, request, consume)
	case "task":
		return iterateTaskLogExport(ctx, task, request, consume)
	default:
		return errors.New("unsupported log category")
	}
}

func iterateCommonLogExport(ctx context.Context, task *model.LogExportTask, request *LogExportRequest, consume func(logExportRow) error) error {
	params := commonLogExportQuery(task, request)
	var cursor *model.LogQueryCursor
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logs, err := model.GetLogsForExport(params, cursor, logExportBatchSize)
		if err != nil {
			return err
		}
		for _, log := range logs {
			if err := consume(buildCommonLogExportRow(log, request)); err != nil {
				return err
			}
		}
		if len(logs) < logExportBatchSize {
			return nil
		}
		last := logs[len(logs)-1]
		cursor = &model.LogQueryCursor{CreatedAt: last.CreatedAt, ID: last.Id, RequestID: last.RequestId}
	}
}

func iterateDrawingLogExport(ctx context.Context, task *model.LogExportTask, request *LogExportRequest, consume func(logExportRow) error) error {
	userID, queryParams := drawingLogExportQuery(task, request)
	beforeID := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logs, err := model.GetMidjourneyForExport(userID, queryParams, beforeID, logExportBatchSize)
		if err != nil {
			return err
		}
		for _, log := range logs {
			if err := consume(buildDrawingLogExportRow(log, request)); err != nil {
				return err
			}
		}
		if len(logs) < logExportBatchSize {
			return nil
		}
		beforeID = logs[len(logs)-1].Id
	}
}

func iterateTaskLogExport(ctx context.Context, task *model.LogExportTask, request *LogExportRequest, consume func(logExportRow) error) error {
	userID, queryParams := taskLogExportQuery(task, request)
	var beforeID int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logs, err := model.GetTasksForExport(userID, queryParams, beforeID, logExportBatchSize)
		if err != nil {
			return err
		}
		if request.Scope == "all" {
			enrichTaskExportUsers(logs)
		}
		for _, log := range logs {
			if err := consume(buildTaskLogExportRow(log, request)); err != nil {
				return err
			}
		}
		if len(logs) < logExportBatchSize {
			return nil
		}
		beforeID = logs[len(logs)-1].ID
	}
}

func enrichTaskExportUsers(tasks []*model.Task) {
	usernames := make(map[int]string)
	for _, task := range tasks {
		if _, ok := usernames[task.UserId]; ok {
			continue
		}
		if user, err := model.GetUserCache(task.UserId); err == nil {
			usernames[task.UserId] = user.Username
		}
	}
	for _, task := range tasks {
		task.Username = usernames[task.UserId]
	}
}

func buildCommonLogExportRow(log *model.Log, request *LogExportRequest) logExportRow {
	raw := make(map[string]any)
	display := make(map[string]string)
	other := make(map[string]any)
	if log.Other != "" {
		_ = common.UnmarshalJsonStr(log.Other, &other)
	}
	createdAt := time.Unix(log.CreatedAt, 0).In(exportLocation(request))
	raw["created_at"] = map[string]any{"timestamp": log.CreatedAt, "type": log.Type}
	display["created_at"] = createdAt.Format("2006-01-02 15:04:05") + " / " + exportValueLabel(request, "log_type", strconv.Itoa(log.Type))
	channelName := log.ChannelName
	username := log.Username
	tokenName := log.TokenName
	groupName := log.Group
	if groupName == "" {
		if value, ok := other["group"].(string); ok {
			groupName = value
		}
	}
	if !request.SensitiveVisible {
		if channelName != "" {
			channelName = "••••"
		}
		if username != "" {
			username = "••••"
		}
		if tokenName != "" {
			tokenName = "••••"
		}
		if groupName != "" {
			groupName = "••••"
		}
	}
	raw["channel"] = map[string]any{"id": log.ChannelId, "name": channelName}
	display["channel"] = formatNamedID(channelName, log.ChannelId)
	raw["user"] = map[string]any{"id": log.UserId, "username": username}
	display["user"] = formatNamedID(username, log.UserId)
	raw["token_name"] = map[string]any{"id": log.TokenId, "name": tokenName, "group": groupName}
	display["token_name"] = valueOrDash(tokenName)
	if groupName != "" {
		display["token_name"] += " / " + groupName
	}
	raw["model_name"] = log.ModelName
	display["model_name"] = valueOrDash(log.ModelName)
	raw["is_stream"] = log.IsStream
	if log.IsStream {
		display["is_stream"] = labelOr(request.Presentation.Labels, "yes", "Yes")
	} else {
		display["is_stream"] = labelOr(request.Presentation.Labels, "no", "No")
	}
	cacheRead := mapNumber(other, "cache_tokens")
	cacheWrite := mapNumber(other, "cache_creation_tokens") + mapNumber(other, "cache_creation_tokens_5m") + mapNumber(other, "cache_creation_tokens_1h")
	raw["prompt_tokens"] = map[string]any{"prompt": log.PromptTokens, "completion": log.CompletionTokens, "cache_read": cacheRead, "cache_write": cacheWrite}
	display["prompt_tokens"] = fmt.Sprintf("%s: %d; %s: %d; %s: %d; %s: %d",
		labelOr(request.Presentation.Labels, "input_tokens", "Input Tokens"), log.PromptTokens,
		labelOr(request.Presentation.Labels, "output_tokens", "Output Tokens"), log.CompletionTokens,
		labelOr(request.Presentation.Labels, "cache_read", "Cache Read"), cacheRead,
		labelOr(request.Presentation.Labels, "cache_write", "Cache Write"), cacheWrite)
	raw["quota"] = log.Quota
	display["quota"] = formatExportQuota(log.Quota, request.Presentation)
	frt := mapNumber(other, "frt")
	tps := 0.0
	if log.UseTime > 0 && log.CompletionTokens > 0 {
		tps = float64(log.CompletionTokens) / float64(log.UseTime)
	}
	raw["use_time"] = map[string]any{"seconds": log.UseTime, "first_response_ms": frt, "tokens_per_second": tps}
	display["use_time"] = fmt.Sprintf("%s: %ds; %s: %dms; %s: %.2f TPS",
		labelOr(request.Presentation.Labels, "duration", "Duration"), log.UseTime,
		labelOr(request.Presentation.Labels, "first_token", "First token"), frt,
		labelOr(request.Presentation.Labels, "throughput", "Throughput"), tps)
	raw["content"] = map[string]any{"content": log.Content, "request_id": log.RequestId, "upstream_request_id": log.UpstreamRequestId}
	display["content"] = valueOrDash(log.Content)
	return logExportRow{Raw: raw, Display: display}
}

func buildDrawingLogExportRow(log *model.Midjourney, request *LogExportRequest) logExportRow {
	raw := make(map[string]any)
	display := make(map[string]string)
	location := exportLocation(request)
	raw["submit_time"] = map[string]any{"submit": log.SubmitTime, "start": log.StartTime, "finish": log.FinishTime, "status": log.Status}
	display["submit_time"] = time.UnixMilli(log.SubmitTime).In(location).Format("2006-01-02 15:04:05") + " / " + exportValueLabel(request, "status", log.Status)
	raw["channel_id"] = log.ChannelId
	display["channel_id"] = formatID(log.ChannelId)
	raw["action"] = log.Action
	display["action"] = exportValueLabel(request, "action", log.Action)
	raw["mj_id"] = log.MjId
	display["mj_id"] = valueOrDash(log.MjId)
	duration := exportDuration(log.SubmitTime, log.FinishTime, 1000)
	raw["duration"] = duration
	display["duration"] = formatDurationSeconds(duration)
	raw["code"] = log.Code
	display["code"] = exportValueLabel(request, "code", strconv.Itoa(log.Code))
	raw["progress"] = log.Progress
	display["progress"] = valueOrDash(log.Progress)
	raw["prompt"] = log.Prompt
	display["prompt"] = valueOrDash(log.Prompt)
	return logExportRow{Raw: raw, Display: display}
}

func buildTaskLogExportRow(log *model.Task, request *LogExportRequest) logExportRow {
	raw := make(map[string]any)
	display := make(map[string]string)
	location := exportLocation(request)
	raw["submit_time"] = map[string]any{"submit": log.SubmitTime, "finish": log.FinishTime}
	display["submit_time"] = time.Unix(log.SubmitTime, 0).In(location).Format("2006-01-02 15:04:05")
	if log.FinishTime > 0 {
		display["submit_time"] += " / " + time.Unix(log.FinishTime, 0).In(location).Format("2006-01-02 15:04:05")
	}
	raw["channel_id"] = log.ChannelId
	display["channel_id"] = formatID(log.ChannelId)
	username := log.Username
	if username == "" {
		username = strconv.Itoa(log.UserId)
	}
	if !request.SensitiveVisible {
		username = "••••"
	}
	raw["user"] = map[string]any{"id": log.UserId, "username": username}
	display["user"] = username
	raw["task_id"] = map[string]any{"id": log.TaskID, "platform": string(log.Platform), "action": log.Action}
	display["task_id"] = fmt.Sprintf("%s / %s / %s", valueOrDash(log.TaskID), exportValueLabel(request, "platform", string(log.Platform)), exportValueLabel(request, "action", log.Action))
	duration := exportDuration(log.SubmitTime, log.FinishTime, 1)
	raw["duration"] = duration
	display["duration"] = formatDurationSeconds(duration)
	raw["status"] = string(log.Status)
	display["status"] = exportValueLabel(request, "status", string(log.Status))
	raw["progress"] = log.Progress
	display["progress"] = valueOrDash(log.Progress)
	raw["fail_reason"] = log.FailReason
	display["fail_reason"] = valueOrDash(log.FailReason)
	return logExportRow{Raw: raw, Display: display}
}

func exportLocation(request *LogExportRequest) *time.Location {
	if request != nil && request.Presentation.TimeZone != "" {
		if location, err := time.LoadLocation(request.Presentation.TimeZone); err == nil {
			return location
		}
	}
	return time.UTC
}

func mapNumber(data map[string]any, key string) int {
	value, ok := data[key]
	if !ok {
		return 0
	}
	switch number := value.(type) {
	case float64:
		return int(number)
	case float32:
		return int(number)
	case int:
		return number
	case int64:
		return int(number)
	case string:
		parsed, _ := strconv.Atoi(number)
		return parsed
	default:
		return 0
	}
}

func exportDuration(start int64, finish int64, divisor int64) float64 {
	if start <= 0 || finish <= start || divisor <= 0 {
		return 0
	}
	return float64(finish-start) / float64(divisor)
}

func formatDurationSeconds(seconds float64) string {
	if seconds <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1fs", seconds)
}

func formatNamedID(name string, id int) string {
	if name == "" && id == 0 {
		return "-"
	}
	if name == "" {
		return formatID(id)
	}
	if id == 0 {
		return name
	}
	return fmt.Sprintf("%s (#%d)", name, id)
}

func formatID(id int) string {
	if id == 0 {
		return "-"
	}
	return fmt.Sprintf("#%d", id)
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func labelOr(labels map[string]string, key string, fallback string) string {
	if value := strings.TrimSpace(labels[key]); value != "" {
		return value
	}
	return fallback
}

func exportValueLabel(request *LogExportRequest, kind string, value string) string {
	return labelOr(request.Presentation.Labels, kind+":"+value, valueOrDash(value))
}

func formatExportQuota(quota int, presentation LogExportPresentation) string {
	if presentation.QuotaDisplayMode == "quota" || presentation.QuotaPerUnit <= 0 {
		return strconv.Itoa(quota)
	}
	rate := presentation.CurrencyRate
	if rate <= 0 {
		rate = 1
	}
	amount := float64(quota) / presentation.QuotaPerUnit * rate
	return fmt.Sprintf("%s%.6f", presentation.CurrencySymbol, amount)
}

func escapeCSVFormula(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed == "" {
		return value
	}
	if trimmed[0] == '-' {
		number, err := strconv.ParseFloat(trimmed, 64)
		if err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
			return value
		}
	}
	switch trimmed[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "|", "\\|")
	value = strings.ReplaceAll(value, "\r\n", "<br>")
	value = strings.ReplaceAll(value, "\r", "<br>")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
}
