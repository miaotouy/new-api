package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetAllTokenUsageData(c *gin.Context) {
	start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || start <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return
	}
	end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || end <= 0 || end < start {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return
	}
	data, err := model.GetTokenUsageData(start, end, c.Query("username"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

func GetUserTokenUsageData(c *gin.Context) {
	start, err := strconv.ParseInt(c.Query("start_timestamp"), 10, 64)
	if err != nil || start <= 0 {
		common.ApiErrorMsg(c, "invalid start_timestamp")
		return
	}
	end, err := strconv.ParseInt(c.Query("end_timestamp"), 10, 64)
	if err != nil || end <= 0 || end < start {
		common.ApiErrorMsg(c, "invalid end_timestamp")
		return
	}
	if end-start > 2592000 {
		common.ApiErrorMsg(c, "时间跨度不能超过 1 个月")
		return
	}
	data, err := model.GetUserTokenUsageData(c.GetInt("id"), start, end)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": data})
}

func CreateTokenUsageBackfillTask(c *gin.Context) {
	task, err := service.StartTokenUsageBackfillTask()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": task.ToResponse()})
}

func GetLatestTokenUsageBackfillTask(c *gin.Context) {
	task, err := model.GetLatestSystemTask(model.SystemTaskTypeTokenUsageBackfill)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil {
		c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": task.ToResponse()})
}

func GetTokenUsageBackfillTask(c *gin.Context) {
	task, err := model.GetSystemTaskByTaskID(c.Param("task_id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if task == nil || task.Type != model.SystemTaskTypeTokenUsageBackfill {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "token usage migration task not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": task.ToResponse()})
}
