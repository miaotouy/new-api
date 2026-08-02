package controller

import (
	"errors"
	"fmt"
	"mime"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const maxActiveLogExportTasksPerUser = 3

func CreateLogExport(c *gin.Context) {
	var request service.LogExportRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "invalid export request")
		return
	}
	userID := c.GetInt("id")
	role := c.GetInt("role")
	if err := service.ValidateLogExportRequest(&request, role); err != nil {
		common.ApiError(c, err)
		return
	}
	snapshotID, err := model.GetLogExportSnapshotID(request.Category)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	request.SnapshotID = snapshotID
	requestData, err := common.Marshal(request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	task, err := model.CreateLogExportTask(userID, request.Category, request.Format, request.Scope, string(requestData), time.Now().UnixMilli(), maxActiveLogExportTasksPerUser)
	if err != nil {
		if errors.Is(err, model.ErrTooManyActiveLogExportTasks) {
			common.ApiErrorMsg(c, err.Error())
			return
		}
		common.ApiError(c, err)
		return
	}
	service.WakeLogExportWorker()
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("log export created: task_id=%s user_id=%d category=%s format=%s scope=%s", task.TaskID, userID, request.Category, request.Format, request.Scope))
	c.JSON(http.StatusAccepted, gin.H{
		"success": true,
		"message": "",
		"data":    task,
	})
}

func ListLogExports(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	tasks, total, err := model.ListUserLogExportTasks(c.GetInt("id"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetItems(tasks)
	pageInfo.SetTotal(int(total))
	common.ApiSuccess(c, pageInfo)
}

func GetLogExport(c *gin.Context) {
	task, err := model.GetUserLogExportTask(c.Param("task_id"), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "export task not found"})
		return
	}
	common.ApiSuccess(c, task)
}

func DeleteLogExport(c *gin.Context) {
	err := model.DeleteUserLogExportTask(c.Param("task_id"), c.GetInt("id"))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "export task not found"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func DownloadLogExport(c *gin.Context) {
	task, err := model.GetUserLogExportTask(c.Param("task_id"), c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "export task not found"})
		return
	}
	if task.Status != model.LogExportStatusSucceeded {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "export task is not ready"})
		return
	}
	if task.ExpiresAt > 0 && task.ExpiresAt <= common.GetTimestamp() {
		c.JSON(http.StatusGone, gin.H{"success": false, "message": "export file has expired"})
		return
	}
	if task.Scope == "all" && c.GetInt("role") < common.RoleAdminUser {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "message": "administrator permission is required"})
		return
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": task.FileName})
	c.Header("Content-Type", task.ContentType)
	c.Header("Content-Disposition", disposition)
	c.Header("Content-Length", fmt.Sprintf("%d", task.FileSize))
	c.Header("Cache-Control", "private, no-store")
	if err := service.StreamLogExportContent(c.Writer, task.TaskID); err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("log export download failed: task_id=%s user_id=%d err=%v", task.TaskID, task.UserID, err))
		if !c.Writer.Written() {
			c.Writer.Header().Del("Content-Disposition")
			c.Writer.Header().Del("Content-Length")
			common.ApiError(c, err)
		}
	}
}
