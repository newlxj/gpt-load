// Package handler provides HTTP handlers for the application
package handler

import (
	"crypto/subtle"
	"net/http"
	"time"

	"aimanager/internal/config"
	"aimanager/internal/encryption"
	"aimanager/internal/i18n"
	"aimanager/internal/services"
	"aimanager/internal/types"

	"github.com/gin-gonic/gin"
	"go.uber.org/dig"
	"gorm.io/gorm"
)

// Server contains dependencies for HTTP handlers
type Server struct {
	DB                         *gorm.DB
	config                     types.ConfigManager
	SettingsManager            *config.SystemSettingsManager
	GroupManager               *services.GroupManager
	GroupService               *services.GroupService
	AggregateGroupService      *services.AggregateGroupService
	KeyManualValidationService *services.KeyManualValidationService
	TaskService                *services.TaskService
	KeyService                 *services.KeyService
	KeyImportService           *services.KeyImportService
	KeyDeleteService           *services.KeyDeleteService
	LogService                 *services.LogService
	CommonHandler              *CommonHandler
	EncryptionSvc              encryption.Service
	LoginLimiter               *services.LoginLimiter
}

// NewServerParams defines the dependencies for the NewServer constructor.
type NewServerParams struct {
	dig.In
	DB                         *gorm.DB
	Config                     types.ConfigManager
	SettingsManager            *config.SystemSettingsManager
	GroupManager               *services.GroupManager
	GroupService               *services.GroupService
	AggregateGroupService      *services.AggregateGroupService
	KeyManualValidationService *services.KeyManualValidationService
	TaskService                *services.TaskService
	KeyService                 *services.KeyService
	KeyImportService           *services.KeyImportService
	KeyDeleteService           *services.KeyDeleteService
	LogService                 *services.LogService
	CommonHandler              *CommonHandler
	EncryptionSvc              encryption.Service
	LoginLimiter               *services.LoginLimiter
}

// NewServer creates a new handler instance with dependencies injected by dig.
func NewServer(params NewServerParams) *Server {
	return &Server{
		DB:                         params.DB,
		config:                     params.Config,
		SettingsManager:            params.SettingsManager,
		GroupManager:               params.GroupManager,
		GroupService:               params.GroupService,
		AggregateGroupService:      params.AggregateGroupService,
		KeyManualValidationService: params.KeyManualValidationService,
		TaskService:                params.TaskService,
		KeyService:                 params.KeyService,
		KeyImportService:           params.KeyImportService,
		KeyDeleteService:           params.KeyDeleteService,
		LogService:                 params.LogService,
		CommonHandler:              params.CommonHandler,
		EncryptionSvc:              params.EncryptionSvc,
		LoginLimiter:               params.LoginLimiter,
	}
}

// LoginRequest represents the login request payload
type LoginRequest struct {
	AuthKey string `json:"auth_key" binding:"required"`
}

// LoginResponse represents the login response
type LoginResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Login handles authentication verification
func (s *Server) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": i18n.Message(c, "auth.invalid_request"),
		})
		return
	}

	// Check if login is locked
	if s.LoginLimiter != nil {
		allowed, remaining := s.LoginLimiter.CheckLogin()
		if !allowed {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"message": i18n.Message(c, "auth.account_locked"),
				"locked": true,
				"remaining_seconds": int(remaining.Seconds()),
			})
			return
		}
	}

	authConfig := s.config.GetAuthConfig()

	isValid := subtle.ConstantTimeCompare([]byte(req.AuthKey), []byte(authConfig.Key)) == 1

	if isValid {
		// Record successful login
		if s.LoginLimiter != nil {
			s.LoginLimiter.RecordSuccess()
		}
		c.JSON(http.StatusOK, LoginResponse{
			Success: true,
			Message: i18n.Message(c, "auth.authentication_successful"),
		})
	} else {
		// Record failed login attempt
		if s.LoginLimiter != nil {
			locked, duration := s.LoginLimiter.RecordFailure()
			if locked {
				c.JSON(http.StatusTooManyRequests, gin.H{
					"success": false,
					"message": i18n.Message(c, "auth.account_locked"),
					"locked": true,
					"lockout_duration_seconds": duration,
				})
				return
			}
		}
		c.JSON(http.StatusUnauthorized, LoginResponse{
			Success: false,
			Message: i18n.Message(c, "auth.authentication_failed"),
		})
	}
}

// Health handles health check requests
func (s *Server) Health(c *gin.Context) {
	uptime := "unknown"
	if startTime, exists := c.Get("serverStartTime"); exists {
		if st, ok := startTime.(time.Time); ok {
			uptime = time.Since(st).String()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"uptime":    uptime,
	})
}
