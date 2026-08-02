package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedLogExportOwner(t *testing.T, id int, role int) {
	t.Helper()
	require.NoError(t, DB.Create(&User{
		Id: id, Username: fmt.Sprintf("export_owner_%d", id), AffCode: fmt.Sprintf("export-aff-%d", id), Role: role, Status: common.UserStatusEnabled,
	}).Error)
}

func TestCreateLogExportTaskEnforcesActiveLimit(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8101, common.RoleCommonUser)

	for range 3 {
		task, err := CreateLogExportTask(8101, "common", "csv", "self", `{}`, 100, 3)
		require.NoError(t, err)
		assert.Equal(t, LogExportStatusPending, task.Status)
	}
	_, err := CreateLogExportTask(8101, "common", "json", "self", `{}`, 100, 3)
	require.ErrorIs(t, err, ErrTooManyActiveLogExportTasks)

	require.NoError(t, DB.Model(&LogExportTask{}).Where("user_id = ?", 8101).
		Update("status", LogExportStatusFailed).Error)
	_, err = CreateLogExportTask(8101, "task", "md", "self", `{}`, 100, 3)
	require.NoError(t, err)
}

func TestRecoverStaleLogExportTasksRetriesOnceThenFails(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8102, common.RoleCommonUser)

	task := &LogExportTask{
		TaskID: "logexp_recovery", UserID: 8102, Category: "common", Format: "csv",
		Scope: "self", Status: LogExportStatusRunning, Attempts: 1,
		LockedBy: "dead-runner", LockedUntil: 99,
	}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Create(&LogExportChunk{TaskID: task.TaskID, Sequence: 0, Data: []byte("partial")}).Error)

	require.NoError(t, RecoverStaleLogExportTasks(100, 100))
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(task).Error)
	assert.Equal(t, LogExportStatusPending, task.Status)
	assert.Empty(t, task.LockedBy)
	var chunks int64
	require.NoError(t, DB.Model(&LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunks).Error)
	assert.Zero(t, chunks)

	claimed, err := ClaimNextLogExportTask("runner-2", 120)
	require.NoError(t, err)
	require.NotNil(t, claimed)
	assert.Equal(t, 2, claimed.Attempts)
	require.NoError(t, DB.Create(&LogExportChunk{TaskID: task.TaskID, Sequence: 0, Data: []byte("partial-again")}).Error)
	require.NoError(t, DB.Model(&LogExportTask{}).Where("task_id = ?", task.TaskID).Updates(map[string]any{
		"locked_until": 99,
	}).Error)

	require.NoError(t, RecoverStaleLogExportTasks(100, 100))
	require.NoError(t, DB.Where("task_id = ?", task.TaskID).First(task).Error)
	assert.Equal(t, LogExportStatusFailed, task.Status)
	assert.Equal(t, "export worker lease expired", task.Error)
	require.NoError(t, DB.Model(&LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunks).Error)
	assert.Zero(t, chunks)
}

func TestRecoverStaleLogExportTasksHonorsBatchLimit(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8108, common.RoleCommonUser)
	for _, taskID := range []string{"logexp_recovery_batch_1", "logexp_recovery_batch_2"} {
		require.NoError(t, DB.Create(&LogExportTask{
			TaskID: taskID, UserID: 8108, Category: "common", Format: "csv",
			Scope: "self", Status: LogExportStatusRunning, Attempts: 1,
			LockedBy: "dead-runner", LockedUntil: 99,
		}).Error)
	}

	require.NoError(t, RecoverStaleLogExportTasks(100, 1))
	var pending int64
	var running int64
	require.NoError(t, DB.Model(&LogExportTask{}).Where("status = ?", LogExportStatusPending).Count(&pending).Error)
	require.NoError(t, DB.Model(&LogExportTask{}).Where("status = ?", LogExportStatusRunning).Count(&running).Error)
	assert.Equal(t, int64(1), pending)
	assert.Equal(t, int64(1), running)

	require.NoError(t, RecoverStaleLogExportTasks(100, 1))
	require.NoError(t, DB.Model(&LogExportTask{}).Where("status = ?", LogExportStatusPending).Count(&pending).Error)
	require.NoError(t, DB.Model(&LogExportTask{}).Where("status = ?", LogExportStatusRunning).Count(&running).Error)
	assert.Equal(t, int64(2), pending)
	assert.Zero(t, running)
}

func TestFailLogExportTaskDoesNotDeleteAnotherWorkersChunks(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8103, common.RoleCommonUser)
	task := &LogExportTask{
		TaskID: "logexp_lease_owner", UserID: 8103, Category: "common", Format: "csv",
		Scope: "self", Status: LogExportStatusRunning, LockedBy: "runner-b", LockedUntil: 200,
	}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Create(&LogExportChunk{TaskID: task.TaskID, Sequence: 0, Data: []byte("owned")}).Error)

	transitioned, err := FailLogExportTask(task.TaskID, "runner-a", errors.New("late worker"))
	require.NoError(t, err)
	assert.False(t, transitioned)
	var chunks int64
	require.NoError(t, DB.Model(&LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunks).Error)
	assert.Equal(t, int64(1), chunks)

	transitioned, err = FailLogExportTask(task.TaskID, "runner-b", errors.New("real failure"))
	require.NoError(t, err)
	assert.True(t, transitioned)
	require.NoError(t, DB.Model(&LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunks).Error)
	assert.Zero(t, chunks)
}

func TestLogExportQueriesUseStableCursorsAndSafeTaskProjection(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8104, common.RoleCommonUser)
	seedLogExportOwner(t, 8105, common.RoleCommonUser)

	logs := []*Log{
		{Id: 1, UserId: 8104, CreatedAt: 101, Type: LogTypeConsume, ModelName: "gpt", RequestId: "r1", Other: `{"admin_info":{"secret":"x"},"audit_info":{"route":"y"}}`},
		{Id: 2, UserId: 8104, CreatedAt: 100, Type: LogTypeConsume, ModelName: "gpt", RequestId: "r2"},
		{Id: 3, UserId: 8104, CreatedAt: 101, Type: LogTypeConsume, ModelName: "gpt", RequestId: "r3"},
		{Id: 4, UserId: 8105, CreatedAt: 102, Type: LogTypeConsume, ModelName: "gpt", RequestId: "other"},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	pageLogs, _, err := GetUserLogs(8104, LogTypeConsume, 0, 101, "gpt", "", 0, 10, "", "", "")
	require.NoError(t, err)
	exportLogs, err := GetLogsForExport(LogQueryParams{UserID: &logs[0].UserId, LogType: LogTypeConsume, EndTimestamp: 101, ModelName: "gpt"}, nil, 10)
	require.NoError(t, err)
	require.Len(t, pageLogs, 3)
	assert.Equal(t, []string{"r3", "r2", "r1"}, []string{pageLogs[0].RequestId, pageLogs[1].RequestId, pageLogs[2].RequestId})
	require.Len(t, exportLogs, 3)
	assert.Equal(t, []string{"r3", "r1", "r2"}, []string{exportLogs[0].RequestId, exportLogs[1].RequestId, exportLogs[2].RequestId})

	params := LogQueryParams{MaxID: 2, UserID: &logs[0].UserId, LogType: LogTypeConsume, EndTimestamp: 101, ModelName: "gpt"}
	first, err := GetLogsForExport(params, nil, 1)
	require.NoError(t, err)
	require.Len(t, first, 1)
	assert.Equal(t, 1, first[0].Id)
	cursor := &LogQueryCursor{CreatedAt: first[0].CreatedAt, ID: first[0].Id, RequestID: first[0].RequestId}
	second, err := GetLogsForExport(params, cursor, 2)
	require.NoError(t, err)
	require.Len(t, second, 1)
	assert.Equal(t, 2, second[0].Id)
	assert.NotContains(t, first[0].Other, "admin_info")
	assert.NotContains(t, first[0].Other, "audit_info")

	private := TaskPrivateData{Key: "secret-key", UpstreamTaskID: "upstream-secret", ResultURL: "https://cdn.example.com/video.mp4"}
	require.NoError(t, DB.Create(&Task{
		UserId: 8104, TaskID: "task-safe", ChannelId: 4242, SubmitTime: 100,
		FailReason: "upstream failure", PrivateData: private,
	}).Error)
	pageTasks := TaskGetAllUserTask(8104, 0, 10, SyncTaskQueryParams{})
	require.Len(t, pageTasks, 1)
	assert.Zero(t, pageTasks[0].ChannelId)
	assert.Equal(t, private.ResultURL, pageTasks[0].GetResultURL())

	adminTasks := TaskGetAllTasks(0, 10, SyncTaskQueryParams{})
	require.Len(t, adminTasks, 1)
	assert.Equal(t, 4242, adminTasks[0].ChannelId)
	assert.Equal(t, private.ResultURL, adminTasks[0].GetResultURL())

	tasks, err := GetTasksForExport(&logs[0].UserId, SyncTaskQueryParams{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	assert.Zero(t, tasks[0].ChannelId)
	assert.Empty(t, tasks[0].PrivateData.Key)
	assert.Empty(t, tasks[0].PrivateData.UpstreamTaskID)
	assert.Empty(t, tasks[0].PrivateData.ResultURL)
}

func TestDrawingAndTaskExportQueriesMatchPageFiltersAndOrder(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8110, common.RoleCommonUser)
	seedLogExportOwner(t, 8111, common.RoleCommonUser)

	midjourneyRows := []*Midjourney{
		{Id: 11, UserId: 8110, MjId: "mj-old", SubmitTime: 1000},
		{Id: 12, UserId: 8110, MjId: "mj-new", SubmitTime: 2000},
		{Id: 13, UserId: 8111, MjId: "mj-other", SubmitTime: 2000},
	}
	require.NoError(t, DB.Create(&midjourneyRows).Error)
	mjParams := TaskQueryParams{StartTimestamp: "900", EndTimestamp: "2100"}
	mjPage := GetAllUserTask(8110, 0, 10, mjParams)
	mjExport, err := GetMidjourneyForExport(&midjourneyRows[0].UserId, mjParams, 0, 10)
	require.NoError(t, err)
	require.Len(t, mjPage, len(mjExport))
	for i := range mjPage {
		assert.Equal(t, mjPage[i].Id, mjExport[i].Id)
	}

	taskRows := []*Task{
		{ID: 21, UserId: 8110, TaskID: "task-old", SubmitTime: 100, Status: TaskStatusSuccess},
		{ID: 22, UserId: 8110, TaskID: "task-new", SubmitTime: 200, Status: TaskStatusSuccess},
		{ID: 23, UserId: 8111, TaskID: "task-other", SubmitTime: 200, Status: TaskStatusSuccess},
	}
	require.NoError(t, DB.Create(&taskRows).Error)
	taskParams := SyncTaskQueryParams{Status: string(TaskStatusSuccess), StartTimestamp: 90, EndTimestamp: 210}
	taskPage := TaskGetAllUserTask(8110, 0, 10, taskParams)
	taskExport, err := GetTasksForExport(&taskRows[0].UserId, taskParams, 0, 10)
	require.NoError(t, err)
	require.Len(t, taskPage, len(taskExport))
	for i := range taskPage {
		assert.Equal(t, taskPage[i].ID, taskExport[i].ID)
	}
}

func TestLogExportOwnershipDeletionAndExpiryCleanup(t *testing.T) {
	truncateTables(t)
	seedLogExportOwner(t, 8106, common.RoleCommonUser)
	seedLogExportOwner(t, 8107, common.RoleCommonUser)

	activeTask := &LogExportTask{
		TaskID: "logexp_active_delete", UserID: 8106, Category: "common", Format: "csv",
		Scope: "self", Status: LogExportStatusPending,
	}
	require.NoError(t, DB.Create(activeTask).Error)
	require.ErrorIs(t, DeleteUserLogExportTask(activeTask.TaskID, 8106), ErrActiveLogExportTask)

	task := &LogExportTask{
		TaskID: "logexp_owner_cleanup", UserID: 8106, Category: "common", Format: "csv",
		Scope: "self", Status: LogExportStatusSucceeded, ExpiresAt: 100,
	}
	require.NoError(t, DB.Create(task).Error)
	require.NoError(t, DB.Create(&LogExportChunk{TaskID: task.TaskID, Sequence: 0, Data: []byte("file")}).Error)

	otherTask, err := GetUserLogExportTask(task.TaskID, 8107)
	require.NoError(t, err)
	assert.Nil(t, otherTask)
	require.ErrorIs(t, DeleteUserLogExportTask(task.TaskID, 8107), gorm.ErrRecordNotFound)

	deleted, err := CleanupExpiredLogExportTasks(99, 10)
	require.NoError(t, err)
	assert.Zero(t, deleted)
	deleted, err = CleanupExpiredLogExportTasks(100, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	reloaded, err := GetLogExportTask(task.TaskID)
	require.NoError(t, err)
	assert.Nil(t, reloaded)
	var chunks int64
	require.NoError(t, DB.Model(&LogExportChunk{}).Where("task_id = ?", task.TaskID).Count(&chunks).Error)
	assert.Zero(t, chunks)
}
