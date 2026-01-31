package handler

import (
	app_errors "aimanager/internal/errors"
	"aimanager/internal/i18n"
	"aimanager/internal/models"
	"aimanager/internal/response"
	"fmt"
	"log"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// LogResponse defines the structure for log entries in the API response
type LogResponse struct {
	models.RequestLog
}

// GetLogs handles fetching request logs with filtering and pagination.
func (s *Server) GetLogs(c *gin.Context) {
	query := s.LogService.GetLogsQuery(c)

	var logs []models.RequestLog
	query = query.Order("timestamp desc")
	pagination, err := response.Paginate(c, query, &logs)
	if err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	// 解密所有日志中的密钥用于前端显示
	for i := range logs {
		if logs[i].KeyValue != "" {
			decryptedValue, err := s.EncryptionSvc.Decrypt(logs[i].KeyValue)
			if err != nil {
				logrus.WithError(err).WithField("log_id", logs[i].ID).Error("Failed to decrypt log key value")
				logs[i].KeyValue = "failed-to-decrypt"
			} else {
				logs[i].KeyValue = decryptedValue
			}
		}
	}

	pagination.Items = logs
	response.Success(c, pagination)
}

// ExportLogs handles exporting filtered log keys to a CSV file.
func (s *Server) ExportLogs(c *gin.Context) {
	filename := fmt.Sprintf("log_keys_export_%s.csv", time.Now().Format("20060102150405"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "text/csv; charset=utf-8")

	// Stream the response
	err := s.LogService.StreamLogKeysToCSV(c, c.Writer)
	if err != nil {
		log.Printf("Failed to stream log keys to CSV: %v", err)
		c.JSON(500, gin.H{"error": i18n.Message(c, "error.export_logs")})
		return
	}
}

// ClearLogs handles deleting logs based on filters (physical deletion).
func (s *Server) ClearLogs(c *gin.Context) {
	// 获取筛选后的日志数量
	count, err := s.LogService.CountFilteredLogs(c)
	if err != nil {
		logrus.WithError(err).Error("Failed to count filtered logs")
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	// 如果没有符合条件的日志，直接返回
	if count == 0 {
		response.SuccessI18n(c, "success.no_logs_to_clear", nil)
		return
	}

	// 执行物理删除
	deletedCount, err := s.LogService.DeleteFilteredLogs(c)
	if err != nil {
		logrus.WithError(err).Error("Failed to delete filtered logs")
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	response.SuccessI18n(c, "success.logs_cleared", map[string]any{
		"count": deletedCount,
	})
}
