package model

import (
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

type LogExportStatus string

const (
	LogExportStatusPending   LogExportStatus = "pending"
	LogExportStatusRunning   LogExportStatus = "running"
	LogExportStatusSucceeded LogExportStatus = "succeeded"
	LogExportStatusFailed    LogExportStatus = "failed"
)

const LogExportRetention = 24 * time.Hour

var (
	ErrTooManyActiveLogExportTasks = errors.New("too many active export tasks")
	ErrActiveLogExportTask         = errors.New("active export task cannot be deleted")
)

type LogExportTask struct {
	ID            int64           `json:"id" gorm:"primaryKey"`
	TaskID        string          `json:"task_id" gorm:"type:varchar(64);uniqueIndex"`
	UserID        int             `json:"user_id" gorm:"index:idx_log_export_user_status_created,priority:1"`
	Category      string          `json:"category" gorm:"type:varchar(16)"`
	Format        string          `json:"format" gorm:"type:varchar(16)"`
	Scope         string          `json:"scope" gorm:"type:varchar(16)"`
	Status        LogExportStatus `json:"status" gorm:"type:varchar(24);index:idx_log_export_user_status_created,priority:2;index"`
	Request       string          `json:"-" gorm:"type:text"`
	Progress      int             `json:"progress"`
	ProcessedRows int             `json:"processed_rows"`
	TotalRows     int             `json:"total_rows"`
	FileName      string          `json:"file_name" gorm:"type:varchar(255)"`
	ContentType   string          `json:"content_type" gorm:"type:varchar(128)"`
	FileSize      int64           `json:"file_size"`
	StoredSize    int64           `json:"stored_size"`
	ChunkCount    int             `json:"chunk_count"`
	Error         string          `json:"error" gorm:"type:text"`
	Attempts      int             `json:"attempts"`
	LockedBy      string          `json:"-" gorm:"type:varchar(128);index"`
	LockedUntil   int64           `json:"-" gorm:"bigint;index"`
	SnapshotAt    int64           `json:"snapshot_at" gorm:"bigint"`
	CreatedAt     int64           `json:"created_at" gorm:"bigint;index:idx_log_export_user_status_created,priority:3"`
	UpdatedAt     int64           `json:"updated_at" gorm:"bigint"`
	CompletedAt   int64           `json:"completed_at" gorm:"bigint"`
	ExpiresAt     int64           `json:"expires_at" gorm:"bigint;index"`
}

type LogExportChunk struct {
	ID         int64  `json:"-" gorm:"primaryKey"`
	TaskID     string `json:"-" gorm:"type:varchar(64);uniqueIndex:idx_log_export_chunk_task_sequence,priority:1"`
	Sequence   int    `json:"-" gorm:"uniqueIndex:idx_log_export_chunk_task_sequence,priority:2"`
	Data       []byte `json:"-"`
	StoredSize int64  `json:"-"`
	RawSize    int64  `json:"-"`
}

func (task *LogExportTask) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if task.CreatedAt == 0 {
		task.CreatedAt = now
	}
	if task.UpdatedAt == 0 {
		task.UpdatedAt = now
	}
	return nil
}

func GenerateLogExportTaskID() (string, error) {
	key, err := common.GenerateRandomCharsKey(32)
	if err != nil {
		return "", err
	}
	return "logexp_" + key, nil
}

func CreateLogExportTask(userID int, category string, format string, scope string, request string, snapshotAt int64, activeLimit int) (*LogExportTask, error) {
	taskID, err := GenerateLogExportTaskID()
	if err != nil {
		return nil, err
	}
	task := &LogExportTask{
		TaskID:     taskID,
		UserID:     userID,
		Category:   category,
		Format:     format,
		Scope:      scope,
		Status:     LogExportStatusPending,
		Request:    request,
		SnapshotAt: snapshotAt,
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var owner User
		if err := lockForUpdate(tx).Select("id").Where("id = ?", userID).First(&owner).Error; err != nil {
			return err
		}
		if activeLimit > 0 {
			var activeCount int64
			if err := tx.Model(&LogExportTask{}).
				Where("user_id = ? AND status IN ?", userID, []LogExportStatus{LogExportStatusPending, LogExportStatusRunning}).
				Count(&activeCount).Error; err != nil {
				return err
			}
			if activeCount >= int64(activeLimit) {
				return ErrTooManyActiveLogExportTasks
			}
		}
		return tx.Create(task).Error
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}

func GetLogExportTask(taskID string) (*LogExportTask, error) {
	var task LogExportTask
	if err := DB.Where("task_id = ?", taskID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func GetUserLogExportTask(taskID string, userID int) (*LogExportTask, error) {
	var task LogExportTask
	if err := DB.Where("task_id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &task, nil
}

func ListUserLogExportTasks(userID int, startIdx int, pageSize int) ([]*LogExportTask, int64, error) {
	var tasks []*LogExportTask
	var total int64
	query := DB.Model(&LogExportTask{}).Where("user_id = ?", userID)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("id desc").Limit(pageSize).Offset(startIdx).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func RecoverStaleLogExportTasks(now int64, limit int) error {
	if limit <= 0 {
		limit = 100
	}
	var staleTasks []*LogExportTask
	if err := DB.Where("status = ? AND locked_until > 0 AND locked_until < ?", LogExportStatusRunning, now).
		Order("id asc").Limit(limit).Find(&staleTasks).Error; err != nil {
		return err
	}
	for _, task := range staleTasks {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			updates := map[string]any{
				"locked_by":    "",
				"locked_until": 0,
				"updated_at":   now,
			}
			if task.Attempts < 2 {
				updates["status"] = LogExportStatusPending
				updates["error"] = ""
			} else {
				updates["status"] = LogExportStatusFailed
				updates["completed_at"] = now
				updates["expires_at"] = now + int64(LogExportRetention.Seconds())
				updates["error"] = "export worker lease expired"
			}
			result := tx.Model(&LogExportTask{}).
				Where("id = ? AND status = ? AND locked_until > 0 AND locked_until < ?", task.ID, LogExportStatusRunning, now).
				Updates(updates)
			if result.Error != nil || result.RowsAffected == 0 {
				return result.Error
			}
			return tx.Where("task_id = ?", task.TaskID).Delete(&LogExportChunk{}).Error
		}); err != nil {
			return fmt.Errorf("recover log export %s: %w", task.TaskID, err)
		}
	}
	return nil
}

func ClaimNextLogExportTask(runnerID string, lockUntil int64) (*LogExportTask, error) {
	var candidates []*LogExportTask
	if err := DB.Where("status = ?", LogExportStatusPending).Order("id asc").Limit(8).Find(&candidates).Error; err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	for _, candidate := range candidates {
		result := DB.Model(&LogExportTask{}).
			Where("id = ? AND status = ?", candidate.ID, LogExportStatusPending).
			Updates(map[string]any{
				"status":       LogExportStatusRunning,
				"locked_by":    runnerID,
				"locked_until": lockUntil,
				"attempts":     gorm.Expr("attempts + 1"),
				"updated_at":   now,
				"error":        "",
			})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}
		var claimed LogExportTask
		if err := DB.Where("id = ?", candidate.ID).First(&claimed).Error; err != nil {
			return nil, err
		}
		return &claimed, nil
	}
	return nil, nil
}

func RenewLogExportLease(taskID string, runnerID string, lockUntil int64) error {
	result := DB.Model(&LogExportTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, LogExportStatusRunning, runnerID).
		Updates(map[string]any{"locked_until": lockUntil, "updated_at": common.GetTimestamp()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("log export lease lost")
	}
	return nil
}

func UpdateLogExportProgress(taskID string, runnerID string, processed int, total int, progress int) error {
	if progress < 0 {
		progress = 0
	}
	if progress > 99 {
		progress = 99
	}
	result := DB.Model(&LogExportTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, LogExportStatusRunning, runnerID).
		Updates(map[string]any{
			"processed_rows": processed,
			"total_rows":     total,
			"progress":       progress,
			"updated_at":     common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("log export lease lost")
	}
	return nil
}

func CreateLogExportChunk(chunk *LogExportChunk) error {
	return DB.Create(chunk).Error
}

func DeleteLogExportChunks(taskID string) error {
	return DB.Where("task_id = ?", taskID).Delete(&LogExportChunk{}).Error
}

func ListLogExportChunks(taskID string, afterSequence int, limit int) ([]*LogExportChunk, error) {
	var chunks []*LogExportChunk
	err := DB.Where("task_id = ? AND sequence > ?", taskID, afterSequence).
		Order("sequence asc").Limit(limit).Find(&chunks).Error
	return chunks, err
}

func FinishLogExportTask(taskID string, runnerID string, fileName string, contentType string, rows int, fileSize int64, storedSize int64, chunkCount int) error {
	now := common.GetTimestamp()
	result := DB.Model(&LogExportTask{}).
		Where("task_id = ? AND status = ? AND locked_by = ?", taskID, LogExportStatusRunning, runnerID).
		Updates(map[string]any{
			"status":         LogExportStatusSucceeded,
			"progress":       100,
			"processed_rows": rows,
			"total_rows":     rows,
			"file_name":      fileName,
			"content_type":   contentType,
			"file_size":      fileSize,
			"stored_size":    storedSize,
			"chunk_count":    chunkCount,
			"locked_by":      "",
			"locked_until":   0,
			"completed_at":   now,
			"expires_at":     now + int64(LogExportRetention.Seconds()),
			"updated_at":     now,
			"error":          "",
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("log export lease lost")
	}
	return nil
}

func FailLogExportTask(taskID string, runnerID string, taskErr error) (bool, error) {
	now := common.GetTimestamp()
	errorText := "export failed"
	if taskErr != nil {
		errorText = taskErr.Error()
	}
	transitioned := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Model(&LogExportTask{}).Where("task_id = ? AND status = ?", taskID, LogExportStatusRunning)
		if runnerID != "" {
			query = query.Where("locked_by = ?", runnerID)
		}
		result := query.Updates(map[string]any{
			"status":       LogExportStatusFailed,
			"locked_by":    "",
			"locked_until": 0,
			"completed_at": now,
			"expires_at":   now + int64(LogExportRetention.Seconds()),
			"updated_at":   now,
			"error":        errorText,
		})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		transitioned = true
		return tx.Where("task_id = ?", taskID).Delete(&LogExportChunk{}).Error
	})
	return transitioned, err
}

func DeleteUserLogExportTask(taskID string, userID int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var task LogExportTask
		if err := tx.Where("task_id = ? AND user_id = ?", taskID, userID).First(&task).Error; err != nil {
			return err
		}
		if task.Status == LogExportStatusPending || task.Status == LogExportStatusRunning {
			return ErrActiveLogExportTask
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&LogExportChunk{}).Error; err != nil {
			return err
		}
		return tx.Delete(&task).Error
	})
}

func CleanupExpiredLogExportTasks(now int64, limit int) (int64, error) {
	var tasks []*LogExportTask
	if limit <= 0 {
		limit = 100
	}
	if err := DB.Where("expires_at > 0 AND expires_at <= ?", now).Order("id asc").Limit(limit).Find(&tasks).Error; err != nil {
		return 0, err
	}
	var deleted int64
	for _, task := range tasks {
		if err := DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("task_id = ?", task.TaskID).Delete(&LogExportChunk{}).Error; err != nil {
				return err
			}
			result := tx.Where("id = ? AND expires_at > 0 AND expires_at <= ?", task.ID, now).Delete(&LogExportTask{})
			if result.Error != nil {
				return result.Error
			}
			deleted += result.RowsAffected
			return nil
		}); err != nil {
			return deleted, fmt.Errorf("cleanup log export %s: %w", task.TaskID, err)
		}
	}
	return deleted, nil
}
