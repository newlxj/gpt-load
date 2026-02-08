package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"aimanager/internal/channel"
	"aimanager/internal/config"
	"aimanager/internal/encryption"
	app_errors "aimanager/internal/errors"
	"aimanager/internal/models"
	"aimanager/internal/utils"

	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// I18nError represents an error that carries translation metadata.
type I18nError struct {
	APIError  *app_errors.APIError
	MessageID string
	Template  map[string]any
}

// Error implements the error interface.
func (e *I18nError) Error() string {
	if e == nil || e.APIError == nil {
		return ""
	}
	return e.APIError.Error()
}

// NewI18nError is a helper to create an I18n-enabled error.
func NewI18nError(apiErr *app_errors.APIError, msgID string, template map[string]any) *I18nError {
	return &I18nError{
		APIError:  apiErr,
		MessageID: msgID,
		Template:  template,
	}
}

// GroupService handles business logic for group operations.
type GroupService struct {
	db                    *gorm.DB
	settingsManager       *config.SystemSettingsManager
	groupManager          *GroupManager
	keyService            *KeyService
	keyImportSvc          *KeyImportService
	encryptionSvc         encryption.Service
	aggregateGroupService *AggregateGroupService
	channelRegistry       []string
}

// NewGroupService constructs a GroupService.
func NewGroupService(
	db *gorm.DB,
	settingsManager *config.SystemSettingsManager,
	groupManager *GroupManager,
	keyService *KeyService,
	keyImportSvc *KeyImportService,
	encryptionSvc encryption.Service,
	aggregateGroupService *AggregateGroupService,
) *GroupService {
	return &GroupService{
		db:                    db,
		settingsManager:       settingsManager,
		groupManager:          groupManager,
		keyService:            keyService,
		keyImportSvc:          keyImportSvc,
		encryptionSvc:         encryptionSvc,
		aggregateGroupService: aggregateGroupService,
		channelRegistry:       channel.GetChannels(),
	}
}

// GroupCreateParams captures all fields required to create a group.
type GroupCreateParams struct {
	Name                string
	DisplayName         string
	Description         string
	GroupType           string
	Upstreams           json.RawMessage
	ChannelType         string
	Sort                int
	TestModel           string
	ValidationEndpoint  string
	ParamOverrides      map[string]any
	ModelRedirectRules  map[string]string
	ModelRedirectStrict bool
	Config              map[string]any
	HeaderRules         []models.HeaderRule
	ProxyKeys           string
	SubGroups           []SubGroupInput
}

// GroupUpdateParams captures updatable fields for a group.
type GroupUpdateParams struct {
	Name                *string
	DisplayName         *string
	Description         *string
	GroupType           *string
	Upstreams           json.RawMessage
	HasUpstreams        bool
	ChannelType         *string
	Sort                *int
	TestModel           string
	HasTestModel        bool
	ValidationEndpoint  *string
	ParamOverrides      map[string]any
	ModelRedirectRules  map[string]string
	ModelRedirectStrict *bool
	Config              map[string]any
	HeaderRules         *[]models.HeaderRule
	ProxyKeys           *string
	SubGroups           *[]SubGroupInput
}

// KeyStats captures aggregated API key statistics for a group.
type KeyStats struct {
	TotalKeys   int64 `json:"total_keys"`
	ActiveKeys  int64 `json:"active_keys"`
	InvalidKeys int64 `json:"invalid_keys"`
}

// RequestStats captures request success and failure ratios over a time window.
type RequestStats struct {
	TotalRequests  int64   `json:"total_requests"`
	FailedRequests int64   `json:"failed_requests"`
	FailureRate    float64 `json:"failure_rate"`
}

// GroupStats aggregates all per-group metrics for dashboard usage.
type GroupStats struct {
	KeyStats    KeyStats     `json:"key_stats"`
	Stats24Hour RequestStats `json:"stats_24_hour"`
	Stats7Day   RequestStats `json:"stats_7_day"`
	Stats30Day  RequestStats `json:"stats_30_day"`
}

// GroupListStats contains simplified statistics for group list display.
type GroupListStats struct {
	Stats24Hour RequestStats `json:"stats_24_hour"`
	Stats7Day   RequestStats `json:"stats_7_day"`
}

// ConfigOption describes a configurable override exposed to clients.
type ConfigOption struct {
	Key          string
	Name         string
	Description  string
	DefaultValue any
}

// CreateGroup validates and persists a new group.
func (s *GroupService) CreateGroup(ctx context.Context, params GroupCreateParams) (*models.Group, error) {
	name := strings.TrimSpace(params.Name)
	if !isValidGroupName(name) {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_group_name", nil)
	}

	channelType := strings.TrimSpace(params.ChannelType)
	if !s.isValidChannelType(channelType) {
		supported := strings.Join(s.channelRegistry, ", ")
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_channel_type", map[string]any{"types": supported})
	}

	groupType := strings.TrimSpace(params.GroupType)
	if groupType == "" {
		groupType = "standard"
	}
	if groupType != "standard" && groupType != "aggregate" {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_group_type", nil)
	}

	var cleanedUpstreams datatypes.JSON
	var testModel string
	var validationEndpoint string

	switch groupType {
	case "aggregate":
		validationEndpoint = ""
		cleanedUpstreams = datatypes.JSON("[]")
		testModel = "-"
	case "standard":
		testModel = strings.TrimSpace(params.TestModel)
		if testModel == "" {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.test_model_required", nil)
		}
		cleaned, err := s.validateAndCleanUpstreams(params.Upstreams)
		if err != nil {
			return nil, err
		}
		cleanedUpstreams = cleaned

		validationEndpoint = strings.TrimSpace(params.ValidationEndpoint)
		if !isValidValidationEndpoint(validationEndpoint) {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_test_path", nil)
		}
	}

	cleanedConfig, err := s.validateAndCleanConfig(params.Config)
	if err != nil {
		return nil, err
	}

	headerRulesJSON, err := s.normalizeHeaderRules(params.HeaderRules)
	if err != nil {
		return nil, err
	}
	if headerRulesJSON == nil {
		headerRulesJSON = datatypes.JSON("[]")
	}

	// Validate model redirect rules for aggregate groups
	if groupType == "aggregate" && len(params.ModelRedirectRules) > 0 {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.aggregate_no_model_redirect", nil)
	}

	// Validate model redirect rules format
	if err := validateModelRedirectRules(params.ModelRedirectRules); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_model_redirect", map[string]any{"error": err.Error()})
	}

	group := models.Group{
		Name:                name,
		DisplayName:         strings.TrimSpace(params.DisplayName),
		Description:         strings.TrimSpace(params.Description),
		GroupType:           groupType,
		Upstreams:           cleanedUpstreams,
		ChannelType:         channelType,
		Sort:                params.Sort,
		TestModel:           testModel,
		ValidationEndpoint:  validationEndpoint,
		ParamOverrides:      params.ParamOverrides,
		ModelRedirectRules:  convertToJSONMap(params.ModelRedirectRules),
		ModelRedirectStrict: params.ModelRedirectStrict,
		Config:              cleanedConfig,
		HeaderRules:         headerRulesJSON,
		ProxyKeys:           strings.TrimSpace(params.ProxyKeys),
	}

	tx := s.db.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return nil, app_errors.ErrDatabase
	}

	if err := tx.Create(&group).Error; err != nil {
		tx.Rollback()
		return nil, app_errors.ParseDBError(err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	if err := s.groupManager.Invalidate(); err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to invalidate group cache")
	}

	return &group, nil
}

// ListGroups returns all groups without sub-group relations.
func (s *GroupService) ListGroups(ctx context.Context) ([]models.Group, error) {
	var groups []models.Group
	if err := s.db.WithContext(ctx).Order("sort asc, id desc").Find(&groups).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	return groups, nil
}

// UpdateGroup validates and updates an existing group.
func (s *GroupService) UpdateGroup(ctx context.Context, id uint, params GroupUpdateParams) (*models.Group, error) {
	var group models.Group
	if err := s.db.WithContext(ctx).First(&group, id).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	tx := s.db.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return nil, app_errors.ErrDatabase
	}
	defer tx.Rollback()

	if params.Name != nil {
		cleanedName := strings.TrimSpace(*params.Name)
		if !isValidGroupName(cleanedName) {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_group_name", nil)
		}
		group.Name = cleanedName
	}

	if params.DisplayName != nil {
		group.DisplayName = strings.TrimSpace(*params.DisplayName)
	}

	if params.Description != nil {
		group.Description = strings.TrimSpace(*params.Description)
	}

	if params.HasUpstreams {
		cleanedUpstreams, err := s.validateAndCleanUpstreams(params.Upstreams)
		if err != nil {
			return nil, err
		}
		group.Upstreams = cleanedUpstreams
	}

	// Check if this group is used as a sub-group in aggregate groups before allowing critical changes
	if group.GroupType != "aggregate" && (params.ChannelType != nil || params.ValidationEndpoint != nil) {
		count, err := s.aggregateGroupService.CountAggregateGroupsUsingSubGroup(ctx, group.ID)
		if err != nil {
			return nil, err
		}

		if count > 0 {
			// Check if ChannelType is being changed
			if params.ChannelType != nil {
				cleanedChannelType := strings.TrimSpace(*params.ChannelType)
				if group.ChannelType != cleanedChannelType {
					return nil, NewI18nError(app_errors.ErrValidation, "validation.sub_group_referenced_cannot_modify",
						map[string]any{"count": count})
				}
			}

			// Check if ValidationEndpoint is being changed
			if params.ValidationEndpoint != nil {
				cleanedValidationEndpoint := strings.TrimSpace(*params.ValidationEndpoint)
				if group.ValidationEndpoint != cleanedValidationEndpoint {
					return nil, NewI18nError(app_errors.ErrValidation, "validation.sub_group_referenced_cannot_modify",
						map[string]any{"count": count})
				}
			}
		}
	}

	if params.ChannelType != nil && group.GroupType != "aggregate" {
		cleanedChannelType := strings.TrimSpace(*params.ChannelType)
		if !s.isValidChannelType(cleanedChannelType) {
			supported := strings.Join(s.channelRegistry, ", ")
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_channel_type", map[string]any{"types": supported})
		}
		group.ChannelType = cleanedChannelType
	}

	if params.Sort != nil {
		group.Sort = *params.Sort
	}

	if params.HasTestModel {
		cleanedTestModel := strings.TrimSpace(params.TestModel)
		if cleanedTestModel == "" {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.test_model_empty", nil)
		}
		group.TestModel = cleanedTestModel
	}

	if params.ParamOverrides != nil {
		group.ParamOverrides = params.ParamOverrides
	}

	// Validate model redirect rules for aggregate groups
	if group.GroupType == "aggregate" && params.ModelRedirectRules != nil && len(params.ModelRedirectRules) > 0 {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.aggregate_no_model_redirect", nil)
	}

	// Validate model redirect rules format
	if params.ModelRedirectRules != nil {
		if err := validateModelRedirectRules(params.ModelRedirectRules); err != nil {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_model_redirect", map[string]any{"error": err.Error()})
		}
		group.ModelRedirectRules = convertToJSONMap(params.ModelRedirectRules)
	}

	if params.ModelRedirectStrict != nil {
		group.ModelRedirectStrict = *params.ModelRedirectStrict
	}

	if params.ValidationEndpoint != nil {
		validationEndpoint := strings.TrimSpace(*params.ValidationEndpoint)
		if !isValidValidationEndpoint(validationEndpoint) {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_test_path", nil)
		}
		group.ValidationEndpoint = validationEndpoint
	}

	if params.Config != nil {
		cleanedConfig, err := s.validateAndCleanConfig(params.Config)
		if err != nil {
			return nil, err
		}
		group.Config = cleanedConfig
	}

	if params.ProxyKeys != nil {
		group.ProxyKeys = strings.TrimSpace(*params.ProxyKeys)
	}

	if params.HeaderRules != nil {
		headerRulesJSON, err := s.normalizeHeaderRules(*params.HeaderRules)
		if err != nil {
			return nil, err
		}
		if headerRulesJSON == nil {
			headerRulesJSON = datatypes.JSON("[]")
		}
		group.HeaderRules = headerRulesJSON
	}

	if err := tx.Save(&group).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, app_errors.ErrDatabase
	}

	if err := s.groupManager.Invalidate(); err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to invalidate group cache")
	}

	return &group, nil
}

// DeleteGroup removes a group and associated resources.
func (s *GroupService) DeleteGroup(ctx context.Context, id uint) error {
	var apiKeys []models.APIKey
	if err := s.db.WithContext(ctx).Where("group_id = ?", id).Find(&apiKeys).Error; err != nil {
		return app_errors.ParseDBError(err)
	}

	keyIDs := make([]uint, 0, len(apiKeys))
	for _, key := range apiKeys {
		keyIDs = append(keyIDs, key.ID)
	}

	tx := s.db.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return app_errors.ErrDatabase
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	var group models.Group
	if err := tx.First(&group, id).Error; err != nil {
		return app_errors.ParseDBError(err)
	}

	if err := tx.Where("group_id = ? OR sub_group_id = ?", id, id).Delete(&models.GroupSubGroup{}).Error; err != nil {
		return app_errors.ParseDBError(err)
	}

	if err := tx.Where("group_id = ?", id).Delete(&models.APIKey{}).Error; err != nil {
		return app_errors.ErrDatabase
	}

	if err := tx.Delete(&models.Group{}, id).Error; err != nil {
		return app_errors.ParseDBError(err)
	}

	if len(keyIDs) > 0 {
		if err := s.keyService.KeyProvider.RemoveKeysFromStore(id, keyIDs); err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{
				"groupID":  id,
				"keyCount": len(keyIDs),
			}).WithError(err).Error("failed to remove keys from memory store, rolling back transaction")
			return NewI18nError(app_errors.ErrDatabase, "error.delete_group_cache", nil)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return app_errors.ErrDatabase
	}
	tx = nil

	if err := s.groupManager.Invalidate(); err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to invalidate group cache")
	}

	return nil
}

// CopyGroup duplicates a group and optionally copies active keys.
func (s *GroupService) CopyGroup(ctx context.Context, sourceGroupID uint, copyKeysOption string) (*models.Group, error) {
	option := strings.TrimSpace(copyKeysOption)
	if option == "" {
		option = "all"
	}
	if option != "none" && option != "valid_only" && option != "all" {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_copy_keys_value", nil)
	}

	var sourceGroup models.Group
	if err := s.db.WithContext(ctx).First(&sourceGroup, sourceGroupID).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	tx := s.db.WithContext(ctx).Begin()
	if err := tx.Error; err != nil {
		return nil, app_errors.ErrDatabase
	}
	defer func() {
		if tx != nil {
			tx.Rollback()
		}
	}()

	newGroup := sourceGroup
	newGroup.ID = 0
	newGroup.Name = s.generateUniqueGroupName(ctx, sourceGroup.Name)
	if sourceGroup.DisplayName != "" {
		newGroup.DisplayName = sourceGroup.DisplayName + " Copy"
	}
	newGroup.CreatedAt = time.Time{}
	newGroup.UpdatedAt = time.Time{}
	newGroup.LastValidatedAt = nil

	if err := tx.Create(&newGroup).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	var sourceKeyValues []string
	if option != "none" {
		var sourceKeys []models.APIKey
		query := tx.Where("group_id = ?", sourceGroupID)
		if option == "valid_only" {
			query = query.Where("status = ?", models.KeyStatusActive)
		}
		if err := query.Find(&sourceKeys).Error; err != nil {
			return nil, app_errors.ParseDBError(err)
		}

		for _, sourceKey := range sourceKeys {
			decryptedKey, err := s.encryptionSvc.Decrypt(sourceKey.KeyValue)
			if err != nil {
				logrus.WithContext(ctx).WithError(err).WithField("key_id", sourceKey.ID).Error("failed to decrypt key during group copy, skipping")
				continue
			}
			sourceKeyValues = append(sourceKeyValues, decryptedKey)
		}
	}

	if err := tx.Commit().Error; err != nil {
		return nil, app_errors.ErrDatabase
	}
	tx = nil

	if err := s.groupManager.Invalidate(); err != nil {
		logrus.WithContext(ctx).WithError(err).Error("failed to invalidate group cache")
	}

	if len(sourceKeyValues) > 0 {
		keysText := strings.Join(sourceKeyValues, "\n")
		if _, err := s.keyImportSvc.StartImportTask(&newGroup, keysText); err != nil {
			logrus.WithContext(ctx).WithFields(logrus.Fields{
				"groupId":  newGroup.ID,
				"keyCount": len(sourceKeyValues),
			}).WithError(err).Error("failed to start async key import task for group copy")
		} else {
			logrus.WithContext(ctx).WithFields(logrus.Fields{
				"groupId":  newGroup.ID,
				"keyCount": len(sourceKeyValues),
			}).Info("started async key import task for group copy")
		}
	}

	return &newGroup, nil
}

// GetGroupStats returns aggregated usage statistics for a group.
func (s *GroupService) GetGroupStats(ctx context.Context, groupID uint) (*GroupStats, error) {
	var group models.Group
	if err := s.db.WithContext(ctx).First(&group, groupID).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	// 根据分组类型选择不同的统计逻辑
	if group.GroupType == "aggregate" {
		return s.getAggregateGroupStats(ctx, groupID)
	}

	return s.getStandardGroupStats(ctx, groupID)
}

// GetGroupListStats returns simplified statistics for group list display (24h and 7d only).
func (s *GroupService) GetGroupListStats(ctx context.Context, groupID uint) (*GroupListStats, error) {
	var group models.Group
	if err := s.db.WithContext(ctx).First(&group, groupID).Error; err != nil {
		return nil, app_errors.ParseDBError(err)
	}

	stats := &GroupListStats{}

	// 根据分组类型选择不同的统计逻辑
	if group.GroupType == "aggregate" {
		return s.getAggregateGroupListStats(ctx, groupID)
	}

	// 获取24小时统计
	stats24h, err := s.queryGroupHourlyStats(ctx, groupID, 24)
	if err != nil {
		logrus.WithError(err).WithField("group_id", groupID).Warn("Failed to fetch 24-hour stats for group list")
		stats.Stats24Hour = RequestStats{}
	} else {
		stats.Stats24Hour = stats24h
	}

	// 获取7天统计
	stats7d, err := s.queryGroupHourlyStats(ctx, groupID, 7*24)
	if err != nil {
		logrus.WithError(err).WithField("group_id", groupID).Warn("Failed to fetch 7-day stats for group list")
		stats.Stats7Day = RequestStats{}
	} else {
		stats.Stats7Day = stats7d
	}

	return stats, nil
}

// queryGroupHourlyStats queries aggregated hourly statistics from group_hourly_stats table
func (s *GroupService) queryGroupHourlyStats(ctx context.Context, groupID uint, hours int) (RequestStats, error) {
	var result struct {
		SuccessCount int64
		FailureCount int64
	}

	now := time.Now()
	currentHour := now.Truncate(time.Hour)
	endTime := currentHour.Add(time.Hour) // Include current hour
	startTime := endTime.Add(-time.Duration(hours) * time.Hour)

	if err := s.db.WithContext(ctx).Model(&models.GroupHourlyStat{}).
		Select("SUM(success_count) as success_count, SUM(failure_count) as failure_count").
		Where("group_id = ? AND time >= ? AND time < ?", groupID, startTime, endTime).
		Scan(&result).Error; err != nil {
		return RequestStats{}, err
	}

	return calculateRequestStats(result.SuccessCount+result.FailureCount, result.FailureCount), nil
}

// queryAggregateGroupHourlyStats queries aggregated hourly statistics for multiple sub-groups
func (s *GroupService) queryAggregateGroupHourlyStats(ctx context.Context, subGroupIDs []uint, hours int) (RequestStats, error) {
	var result struct {
		SuccessCount int64
		FailureCount int64
	}

	now := time.Now()
	currentHour := now.Truncate(time.Hour)
	endTime := currentHour.Add(time.Hour) // Include current hour
	startTime := endTime.Add(-time.Duration(hours) * time.Hour)

	if err := s.db.WithContext(ctx).Model(&models.GroupHourlyStat{}).
		Select("SUM(success_count) as success_count, SUM(failure_count) as failure_count").
		Where("group_id IN ? AND time >= ? AND time < ?", subGroupIDs, startTime, endTime).
		Scan(&result).Error; err != nil {
		return RequestStats{}, err
	}

	return calculateRequestStats(result.SuccessCount+result.FailureCount, result.FailureCount), nil
}

// fetchKeyStats retrieves API key statistics for a group
func (s *GroupService) fetchKeyStats(ctx context.Context, groupID uint) (KeyStats, error) {
	var totalKeys, activeKeys int64

	if err := s.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("group_id = ?", groupID).
		Count(&totalKeys).Error; err != nil {
		return KeyStats{}, fmt.Errorf("failed to get total keys: %w", err)
	}

	if err := s.db.WithContext(ctx).Model(&models.APIKey{}).
		Where("group_id = ? AND status = ?", groupID, models.KeyStatusActive).
		Count(&activeKeys).Error; err != nil {
		return KeyStats{}, fmt.Errorf("failed to get active keys: %w", err)
	}

	return KeyStats{
		TotalKeys:   totalKeys,
		ActiveKeys:  activeKeys,
		InvalidKeys: totalKeys - activeKeys,
	}, nil
}

// fetchRequestStats retrieves request statistics for multiple time periods
func (s *GroupService) fetchRequestStats(ctx context.Context, groupID uint, stats *GroupStats) []error {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	// Define time periods and their corresponding setters
	timePeriods := []struct {
		hours  int
		name   string
		setter func(RequestStats)
	}{
		{24, "24-hour", func(r RequestStats) { stats.Stats24Hour = r }},
		{7 * 24, "7-day", func(r RequestStats) { stats.Stats7Day = r }},
		{30 * 24, "30-day", func(r RequestStats) { stats.Stats30Day = r }},
	}

	// Fetch statistics for each time period concurrently
	for _, period := range timePeriods {
		wg.Add(1)
		go func(hours int, name string, setter func(RequestStats)) {
			defer wg.Done()

			res, err := s.queryGroupHourlyStats(ctx, groupID, hours)
			if err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("failed to get %s stats: %w", name, err))
				mu.Unlock()
				return
			}

			mu.Lock()
			setter(res)
			mu.Unlock()
		}(period.hours, period.name, period.setter)
	}

	wg.Wait()
	return errs
}

func (s *GroupService) getStandardGroupStats(ctx context.Context, groupID uint) (*GroupStats, error) {
	stats := &GroupStats{}
	var allErrors []error

	// Fetch key statistics (only for standard groups)
	keyStats, err := s.fetchKeyStats(ctx, groupID)
	if err != nil {
		allErrors = append(allErrors, err)
		// Log error but continue to fetch request stats
		logrus.WithContext(ctx).WithError(err).Warn("failed to fetch key stats, continuing with request stats")
	} else {
		stats.KeyStats = keyStats
	}

	// Fetch request statistics (common for all groups)
	if errs := s.fetchRequestStats(ctx, groupID, stats); len(errs) > 0 {
		allErrors = append(allErrors, errs...)
	}

	// Handle errors
	if len(allErrors) > 0 {
		logrus.WithContext(ctx).WithError(allErrors[0]).Error("errors occurred while fetching group stats")
		// Return partial stats if we have some data
		if stats.Stats24Hour.TotalRequests > 0 || stats.Stats7Day.TotalRequests > 0 || stats.Stats30Day.TotalRequests > 0 {
			return stats, nil
		}
		return nil, NewI18nError(app_errors.ErrDatabase, "database.group_stats_failed", nil)
	}

	return stats, nil
}

// getAggregateGroupListStats returns simplified statistics for aggregate group list display.
func (s *GroupService) getAggregateGroupListStats(ctx context.Context, groupID uint) (*GroupListStats, error) {
	subGroupIDs, err := s.aggregateGroupService.GetSubGroupIDs(ctx, groupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get sub-group IDs: %w", err)
	}

	if len(subGroupIDs) == 0 {
		return &GroupListStats{}, nil
	}

	stats := &GroupListStats{}

	// 聚合24小时统计
	stats24h, err := s.queryAggregateGroupHourlyStats(ctx, subGroupIDs, 24)
	if err != nil {
		logrus.WithError(err).WithField("group_id", groupID).Warn("Failed to fetch 24-hour stats for aggregate group list")
		stats.Stats24Hour = RequestStats{}
	} else {
		stats.Stats24Hour = stats24h
	}

	// 聚合7天统计
	stats7d, err := s.queryAggregateGroupHourlyStats(ctx, subGroupIDs, 7*24)
	if err != nil {
		logrus.WithError(err).WithField("group_id", groupID).Warn("Failed to fetch 7-day stats for aggregate group list")
		stats.Stats7Day = RequestStats{}
	} else {
		stats.Stats7Day = stats7d
	}

	return stats, nil
}

func (s *GroupService) getAggregateGroupStats(ctx context.Context, groupID uint) (*GroupStats, error) {
	stats := &GroupStats{}

	// Aggregate groups only need request statistics, not key statistics
	if errs := s.fetchRequestStats(ctx, groupID, stats); len(errs) > 0 {
		logrus.WithContext(ctx).WithError(errs[0]).Error("errors occurred while fetching aggregate group stats")
		// Return partial stats if we have some data
		if stats.Stats24Hour.TotalRequests > 0 || stats.Stats7Day.TotalRequests > 0 || stats.Stats30Day.TotalRequests > 0 {
			return stats, nil
		}
		return nil, NewI18nError(app_errors.ErrDatabase, "database.group_stats_failed", nil)
	}

	return stats, nil
}

// GetGroupConfigOptions returns metadata describing available overrides.
func (s *GroupService) GetGroupConfigOptions() ([]ConfigOption, error) {
	defaultSettings := utils.DefaultSystemSettings()
	settingDefinitions := utils.GenerateSettingsMetadata(&defaultSettings)
	defMap := make(map[string]models.SystemSettingInfo)
	for _, def := range settingDefinitions {
		defMap[def.Key] = def
	}

	currentSettings := s.settingsManager.GetSettings()
	currentSettingsValue := reflect.ValueOf(currentSettings)
	currentSettingsType := currentSettingsValue.Type()
	jsonToFieldMap := make(map[string]string)
	for i := 0; i < currentSettingsType.NumField(); i++ {
		field := currentSettingsType.Field(i)
		jsonTag := strings.Split(field.Tag.Get("json"), ",")[0]
		if jsonTag != "" {
			jsonToFieldMap[jsonTag] = field.Name
		}
	}

	groupConfigType := reflect.TypeOf(models.GroupConfig{})
	var options []ConfigOption
	for i := 0; i < groupConfigType.NumField(); i++ {
		field := groupConfigType.Field(i)
		jsonTag := field.Tag.Get("json")
		key := strings.Split(jsonTag, ",")[0]
		if key == "" || key == "-" {
			continue
		}

		definition, ok := defMap[key]
		if !ok {
			continue
		}

		var defaultValue any
		if fieldName, ok := jsonToFieldMap[key]; ok {
			defaultValue = currentSettingsValue.FieldByName(fieldName).Interface()
		}

		options = append(options, ConfigOption{
			Key:          key,
			Name:         definition.Name,
			Description:  definition.Description,
			DefaultValue: defaultValue,
		})
	}

	return options, nil
}

// validateAndCleanConfig verifies GroupConfig overrides.
func (s *GroupService) validateAndCleanConfig(configMap map[string]any) (map[string]any, error) {
	if configMap == nil {
		return nil, nil
	}

	var tempGroupConfig models.GroupConfig
	groupConfigType := reflect.TypeOf(tempGroupConfig)
	validFields := make(map[string]bool)
	for i := 0; i < groupConfigType.NumField(); i++ {
		jsonTag := groupConfigType.Field(i).Tag.Get("json")
		fieldName := strings.Split(jsonTag, ",")[0]
		if fieldName != "" && fieldName != "-" {
			validFields[fieldName] = true
		}
	}

	for key := range configMap {
		if !validFields[key] {
			message := fmt.Sprintf("unknown config field: '%s'", key)
			return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": message})
		}
	}

	// 验证限流配置
	if err := s.validateRateLimitConfig(configMap); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	// 限流配置字段（不参与 settingsManager 的验证）
	rateLimitFields := map[string]bool{
		"expires_at":             true,
		"max_requests_per_hour":  true,
		"max_requests_per_month": true,
	}

	// 过滤掉限流配置字段后再进行 settingsManager 验证
	configForSettingsValidation := make(map[string]any)
	for k, v := range configMap {
		if !rateLimitFields[k] {
			configForSettingsValidation[k] = v
		}
	}

	if err := s.settingsManager.ValidateGroupConfigOverrides(configForSettingsValidation); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	configBytes, err := json.Marshal(configMap)
	if err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	var validatedConfig models.GroupConfig
	if err := json.Unmarshal(configBytes, &validatedConfig); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	validatedBytes, err := json.Marshal(validatedConfig)
	if err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	var finalMap map[string]any
	if err := json.Unmarshal(validatedBytes, &finalMap); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "error.invalid_config_format", map[string]any{"error": err.Error()})
	}

	return finalMap, nil
}

// validateRateLimitConfig 验证限流和有效期配置
func (s *GroupService) validateRateLimitConfig(configMap map[string]any) error {
	if configMap == nil {
		return nil
	}

	// 验证 expires_at 字段
	if expiresAtVal, exists := configMap["expires_at"]; exists && expiresAtVal != nil {
		switch v := expiresAtVal.(type) {
		case string:
			// 前端发送的格式: 2006-01-02 15:04:05
			parsedTime, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local)
			if err != nil {
				return fmt.Errorf("invalid expires_at format '%s': %w", v, err)
			}
			_ = parsedTime // 验证通过
		default:
			return fmt.Errorf("expires_at must be a string in ISO8601 format")
		}
	}

	// 验证 max_requests_per_hour 字段
	if hourlyVal, exists := configMap["max_requests_per_hour"]; exists && hourlyVal != nil {
		switch v := hourlyVal.(type) {
		case float64:
			if v < 0 {
				return fmt.Errorf("max_requests_per_hour must be >= 0")
			}
		case int:
			if v < 0 {
				return fmt.Errorf("max_requests_per_hour must be >= 0")
			}
		default:
			return fmt.Errorf("max_requests_per_hour must be a number")
		}
	}

	// 验证 max_requests_per_month 字段
	if monthlyVal, exists := configMap["max_requests_per_month"]; exists && monthlyVal != nil {
		switch v := monthlyVal.(type) {
		case float64:
			if v < 0 {
				return fmt.Errorf("max_requests_per_month must be >= 0")
			}
		case int:
			if v < 0 {
				return fmt.Errorf("max_requests_per_month must be >= 0")
			}
		default:
			return fmt.Errorf("max_requests_per_month must be a number")
		}
	}

	return nil
}

// normalizeHeaderRules deduplicates and normalises header rules.
func (s *GroupService) normalizeHeaderRules(rules []models.HeaderRule) (datatypes.JSON, error) {
	if len(rules) == 0 {
		return nil, nil
	}

	normalized := make([]models.HeaderRule, 0, len(rules))
	seenKeys := make(map[string]bool)

	for _, rule := range rules {
		key := strings.TrimSpace(rule.Key)
		if key == "" {
			continue
		}
		canonicalKey := http.CanonicalHeaderKey(key)
		if seenKeys[canonicalKey] {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.duplicate_header", map[string]any{"key": canonicalKey})
		}
		seenKeys[canonicalKey] = true
		normalized = append(normalized, models.HeaderRule{Key: canonicalKey, Value: rule.Value, Action: rule.Action})
	}

	if len(normalized) == 0 {
		return nil, nil
	}

	headerRulesBytes, err := json.Marshal(normalized)
	if err != nil {
		return nil, NewI18nError(app_errors.ErrInternalServer, "error.process_header_rules", map[string]any{"error": err.Error()})
	}

	return datatypes.JSON(headerRulesBytes), nil
}

// validateAndCleanUpstreams validates upstream definitions.
func (s *GroupService) validateAndCleanUpstreams(upstreams json.RawMessage) (datatypes.JSON, error) {
	if len(upstreams) == 0 {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": "upstreams field is required"})
	}

	var defs []struct {
		URL    string `json:"url"`
		Weight int    `json:"weight"`
	}
	if err := json.Unmarshal(upstreams, &defs); err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": err.Error()})
	}

	if len(defs) == 0 {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": "at least one upstream is required"})
	}

	hasActiveUpstream := false
	for i := range defs {
		defs[i].URL = strings.TrimSpace(defs[i].URL)
		if defs[i].URL == "" {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": "upstream URL cannot be empty"})
		}
		if !strings.HasPrefix(defs[i].URL, "http://") && !strings.HasPrefix(defs[i].URL, "https://") {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": fmt.Sprintf("invalid URL format for upstream: %s", defs[i].URL)})
		}
		if defs[i].Weight < 0 {
			return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": "upstream weight must be a non-negative integer"})
		}
		if defs[i].Weight > 0 {
			hasActiveUpstream = true
		}
	}

	if !hasActiveUpstream {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": "at least one upstream must have a weight greater than 0"})
	}

	cleanedUpstreams, err := json.Marshal(defs)
	if err != nil {
		return nil, NewI18nError(app_errors.ErrValidation, "validation.invalid_upstreams", map[string]any{"error": err.Error()})
	}

	return datatypes.JSON(cleanedUpstreams), nil
}

func calculateRequestStats(total, failed int64) RequestStats {
	stats := RequestStats{
		TotalRequests:  total,
		FailedRequests: failed,
	}
	if total > 0 {
		rate := float64(failed) / float64(total)
		stats.FailureRate = math.Round(rate*10000) / 10000
	}
	return stats
}

func (s *GroupService) generateUniqueGroupName(ctx context.Context, baseName string) string {
	var groups []models.Group
	if err := s.db.WithContext(ctx).Select("name").Find(&groups).Error; err != nil {
		return baseName + "_copy"
	}

	existingNames := make(map[string]bool, len(groups))
	for _, group := range groups {
		existingNames[group.Name] = true
	}

	copyName := baseName + "_copy"
	if !existingNames[copyName] {
		return copyName
	}

	for i := 2; i <= 1000; i++ {
		candidate := fmt.Sprintf("%s_copy_%d", baseName, i)
		if !existingNames[candidate] {
			return candidate
		}
	}

	return copyName
}

// isValidGroupName validates the group name.
func isValidGroupName(name string) bool {
	if name == "" {
		return false
	}
	match, _ := regexp.MatchString("^[a-z0-9_-]{1,100}$", name)
	return match
}

// isValidValidationEndpoint validates custom validation endpoint path.
func isValidValidationEndpoint(endpoint string) bool {
	if endpoint == "" {
		return true
	}
	if !strings.HasPrefix(endpoint, "/") {
		return false
	}
	if strings.Contains(endpoint, "://") {
		return false
	}
	return true
}

// isValidChannelType checks channel type against registered channels.
func (s *GroupService) isValidChannelType(channelType string) bool {
	for _, t := range s.channelRegistry {
		if t == channelType {
			return true
		}
	}
	return false
}

// convertToJSONMap converts a map[string]string to datatypes.JSONMap
func convertToJSONMap(input map[string]string) datatypes.JSONMap {
	if len(input) == 0 {
		return datatypes.JSONMap{}
	}

	result := make(datatypes.JSONMap)
	for k, v := range input {
		result[k] = v
	}
	return result
}

// validateModelRedirectRules validates the format and content of model redirect rules
func validateModelRedirectRules(rules map[string]string) error {
	if len(rules) == 0 {
		return nil
	}

	for key, value := range rules {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return fmt.Errorf("model name cannot be empty")
		}
	}

	return nil
}

// CheckRateLimit 检查分组是否超过限流或过期
func (s *GroupService) CheckRateLimit(ctx context.Context, groupID uint) *app_errors.RateLimitError {
	var group models.Group
	if err := s.db.WithContext(ctx).Select("config").First(&group, groupID).Error; err != nil {
		return nil // 如果获取分组失败，不做限流检查
	}

	// 解析配置
	var config models.GroupConfig
	if group.Config != nil {
		configBytes, _ := json.Marshal(group.Config)
		_ = json.Unmarshal(configBytes, &config)
	}

	now := time.Now()

	// 1. 检查是否过期
	if config.ExpiresAt != nil && *config.ExpiresAt != "" {
		expiresAt, err := time.ParseInLocation("2006-01-02 15:04:05", *config.ExpiresAt, time.Local)
		if err == nil && now.After(expiresAt) {
			return &app_errors.RateLimitError{
				Reason:  "expired",
				ResetAt: expiresAt,
			}
		}
	}

	// 2. 检查每小时限制
	if config.MaxRequestsPerHour != nil && *config.MaxRequestsPerHour > 0 {
		currentHour := now.Truncate(time.Hour)
		var hourlyStat models.GroupHourlyStat
		if err := s.db.WithContext(ctx).
			Where("group_id = ? AND time = ?", groupID, currentHour).
			First(&hourlyStat).Error; err == nil {
			totalRequests := hourlyStat.SuccessCount + hourlyStat.FailureCount
			if totalRequests >= int64(*config.MaxRequestsPerHour) {
				return &app_errors.RateLimitError{
					Reason:  "hourly_limit",
					Limit:   int64(*config.MaxRequestsPerHour),
					Used:    totalRequests,
					ResetAt: currentHour.Add(time.Hour),
				}
			}
		}
	}

	// 3. 检查每月限制
	if config.MaxRequestsPerMonth != nil && *config.MaxRequestsPerMonth > 0 {
		currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		var monthlyStat models.GroupMonthlyStat
		if err := s.db.WithContext(ctx).
			Where("group_id = ? AND month = ?", groupID, currentMonth).
			First(&monthlyStat).Error; err == nil {
			if monthlyStat.RequestCount >= int64(*config.MaxRequestsPerMonth) {
				// 计算下个月初作为重置时间
				nextMonth := currentMonth.AddDate(0, 1, 0)
				return &app_errors.RateLimitError{
					Reason:  "monthly_limit",
					Limit:   int64(*config.MaxRequestsPerMonth),
					Used:    monthlyStat.RequestCount,
					ResetAt: nextMonth,
				}
			}
		}
	}

	return nil
}

// IncrementGroupMonthlyStat 增加分组的月度统计
func (s *GroupService) IncrementGroupMonthlyStat(ctx context.Context, groupID uint, isSuccess bool) error {
	now := time.Now()
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// 使用 ON DUPLICATE KEY UPDATE 或类似机制
	// 先尝试查找现有记录
	var stat models.GroupMonthlyStat
	result := s.db.WithContext(ctx).
		Where("group_id = ? AND month = ?", groupID, currentMonth).
		First(&stat)

	if result.Error != nil && errors.Is(result.Error, gorm.ErrRecordNotFound) {
		// 创建新记录
		stat = models.GroupMonthlyStat{
			Month:        currentMonth,
			GroupID:      groupID,
			RequestCount: 1,
		}
		if isSuccess {
			stat.SuccessCount = 1
		} else {
			stat.FailureCount = 1
		}
		return s.db.WithContext(ctx).Create(&stat).Error
	}

	if result.Error != nil {
		return result.Error
	}

	// 更新现有记录
	updates := map[string]interface{}{
		"request_count": gorm.Expr("request_count + ?", 1),
	}
	if isSuccess {
		updates["success_count"] = gorm.Expr("success_count + ?", 1)
	} else {
		updates["failure_count"] = gorm.Expr("failure_count + ?", 1)
	}

	return s.db.WithContext(ctx).
		Model(&models.GroupMonthlyStat{}).
		Where("group_id = ? AND month = ?", groupID, currentMonth).
		Updates(updates).Error
}
