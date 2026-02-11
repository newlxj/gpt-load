// Package handler provides HTTP handlers for the application
package handler

import (
	"encoding/json"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	app_errors "aimanager/internal/errors"
	"aimanager/internal/i18n"
	"aimanager/internal/models"
	"aimanager/internal/response"
	"aimanager/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func (s *Server) handleGroupError(c *gin.Context, err error) bool {
	if err == nil {
		return false
	}

	if svcErr, ok := err.(*services.I18nError); ok {
		if svcErr.Template != nil {
			response.ErrorI18nFromAPIError(c, svcErr.APIError, svcErr.MessageID, svcErr.Template)
		} else {
			response.ErrorI18nFromAPIError(c, svcErr.APIError, svcErr.MessageID)
		}
		return true
	}

	if apiErr, ok := err.(*app_errors.APIError); ok {
		response.Error(c, apiErr)
		return true
	}

	logrus.WithContext(c.Request.Context()).WithError(err).Error("unexpected group service error")
	response.Error(c, app_errors.ErrInternalServer)
	return true
}

// GroupCreateRequest defines the payload for creating a group.
type GroupCreateRequest struct {
	Name                string              `json:"name"`
	DisplayName         string              `json:"display_name"`
	Description         string              `json:"description"`
	GroupType           string              `json:"group_type"` // 'standard' or 'aggregate'
	Upstreams           json.RawMessage     `json:"upstreams"`
	ChannelType         string              `json:"channel_type"`
	Sort                int                 `json:"sort"`
	TestModel           string              `json:"test_model"`
	ValidationEndpoint  string              `json:"validation_endpoint"`
	ParamOverrides      map[string]any      `json:"param_overrides"`
	ModelRedirectRules  map[string]string   `json:"model_redirect_rules"`
	ModelRedirectStrict bool                `json:"model_redirect_strict"`
	Config              map[string]any      `json:"config"`
	HeaderRules         []models.HeaderRule `json:"header_rules"`
	ProxyKeys           string              `json:"proxy_keys"`
}

// CreateGroup handles the creation of a new group.
func (s *Server) CreateGroup(c *gin.Context) {
	var req GroupCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	params := services.GroupCreateParams{
		Name:                req.Name,
		DisplayName:         req.DisplayName,
		Description:         req.Description,
		GroupType:           req.GroupType,
		Upstreams:           req.Upstreams,
		ChannelType:         req.ChannelType,
		Sort:                req.Sort,
		TestModel:           req.TestModel,
		ValidationEndpoint:  req.ValidationEndpoint,
		ParamOverrides:      req.ParamOverrides,
		ModelRedirectRules:  req.ModelRedirectRules,
		ModelRedirectStrict: req.ModelRedirectStrict,
		Config:              req.Config,
		HeaderRules:         req.HeaderRules,
		ProxyKeys:           req.ProxyKeys,
	}

	group, err := s.GroupService.CreateGroup(c.Request.Context(), params)
	if s.handleGroupError(c, err) {
		return
	}

	response.Success(c, s.newGroupResponse(group))
}

// ListGroups handles listing all groups.
func (s *Server) ListGroups(c *gin.Context) {
	groups, err := s.GroupService.ListGroups(c.Request.Context())
	if s.handleGroupError(c, err) {
		return
	}

	groupResponses := make([]GroupResponse, 0, len(groups))
	for i := range groups {
		groupResp := s.newGroupResponse(&groups[i])

		// 获取分组的统计信息（24小时、7天和30天）
		stats, err := s.GroupService.GetGroupListStats(c.Request.Context(), groups[i].ID)
		if err == nil && stats != nil {
			groupResp.Stats24Hour = &stats.Stats24Hour
			groupResp.Stats7Day = &stats.Stats7Day
			groupResp.Stats30Day = &stats.Stats30Day
		}

		groupResponses = append(groupResponses, *groupResp)
	}

	response.Success(c, groupResponses)
}

// GroupUpdateRequest defines the payload for updating a group.
// Using a dedicated struct avoids issues with zero values being ignored by GORM's Update.
type GroupUpdateRequest struct {
	Name                *string             `json:"name,omitempty"`
	DisplayName         *string             `json:"display_name,omitempty"`
	Description         *string             `json:"description,omitempty"`
	GroupType           *string             `json:"group_type,omitempty"`
	Upstreams           json.RawMessage     `json:"upstreams"`
	ChannelType         *string             `json:"channel_type,omitempty"`
	Sort                *int                `json:"sort"`
	TestModel           string              `json:"test_model"`
	ValidationEndpoint  *string             `json:"validation_endpoint,omitempty"`
	ParamOverrides      map[string]any      `json:"param_overrides"`
	ModelRedirectRules  map[string]string   `json:"model_redirect_rules"`
	ModelRedirectStrict *bool               `json:"model_redirect_strict"`
	Config              map[string]any      `json:"config"`
	HeaderRules         []models.HeaderRule `json:"header_rules"`
	ProxyKeys           *string             `json:"proxy_keys,omitempty"`
}

// UpdateGroup handles updating an existing group.
func (s *Server) UpdateGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	var req GroupUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	params := services.GroupUpdateParams{
		Name:                req.Name,
		DisplayName:         req.DisplayName,
		Description:         req.Description,
		GroupType:           req.GroupType,
		ChannelType:         req.ChannelType,
		Sort:                req.Sort,
		ValidationEndpoint:  req.ValidationEndpoint,
		ParamOverrides:      req.ParamOverrides,
		ModelRedirectRules:  req.ModelRedirectRules,
		ModelRedirectStrict: req.ModelRedirectStrict,
		Config:              req.Config,
		ProxyKeys:           req.ProxyKeys,
	}

	if req.Upstreams != nil {
		params.Upstreams = req.Upstreams
		params.HasUpstreams = true
	}

	if req.TestModel != "" {
		params.TestModel = req.TestModel
		params.HasTestModel = true
	}

	if req.HeaderRules != nil {
		rules := req.HeaderRules
		params.HeaderRules = &rules
	}

	group, err := s.GroupService.UpdateGroup(c.Request.Context(), uint(id), params)
	if s.handleGroupError(c, err) {
		return
	}

	response.Success(c, s.newGroupResponse(group))
}

// GroupResponse defines the structure for a group response, excluding sensitive or large fields.
type GroupResponse struct {
	ID                  uint                `json:"id"`
	Name                string              `json:"name"`
	Endpoint            string              `json:"endpoint"`
	DisplayName         string              `json:"display_name"`
	Description         string              `json:"description"`
	GroupType           string              `json:"group_type"`
	Upstreams           datatypes.JSON      `json:"upstreams"`
	ChannelType         string              `json:"channel_type"`
	Sort                int                 `json:"sort"`
	TestModel           string              `json:"test_model"`
	ValidationEndpoint  string              `json:"validation_endpoint"`
	ParamOverrides      datatypes.JSONMap   `json:"param_overrides"`
	ModelRedirectRules  datatypes.JSONMap   `json:"model_redirect_rules"`
	ModelRedirectStrict bool                `json:"model_redirect_strict"`
	Config              datatypes.JSONMap   `json:"config"`
	HeaderRules         []models.HeaderRule `json:"header_rules"`
	ProxyKeys           string              `json:"proxy_keys"`
	LastValidatedAt     *time.Time          `json:"last_validated_at"`
	CreatedAt           time.Time           `json:"created_at"`
	UpdatedAt           time.Time           `json:"updated_at"`
	// 统计信息
	Stats24Hour         *services.RequestStats `json:"stats_24_hour,omitempty"`
	Stats7Day           *services.RequestStats `json:"stats_7_day,omitempty"`
	Stats30Day          *services.RequestStats `json:"stats_30_day,omitempty"`
}

// newGroupResponse creates a new GroupResponse from a models.Group.
func (s *Server) newGroupResponse(group *models.Group) *GroupResponse {
	appURL := s.SettingsManager.GetAppUrl()
	endpoint := ""
	if appURL != "" {
		u, err := url.Parse(appURL)
		if err == nil {
			u.Path = strings.TrimRight(u.Path, "/") + "/" + group.Name  //proxy/
			endpoint = u.String()
		}
	}

	// Parse header rules from JSON
	var headerRules []models.HeaderRule
	if len(group.HeaderRules) > 0 {
		if err := json.Unmarshal(group.HeaderRules, &headerRules); err != nil {
			logrus.WithError(err).Error("Failed to unmarshal header rules")
			headerRules = make([]models.HeaderRule, 0)
		}
	}

	return &GroupResponse{
		ID:                  group.ID,
		Name:                group.Name,
		Endpoint:            endpoint,
		DisplayName:         group.DisplayName,
		Description:         group.Description,
		GroupType:           group.GroupType,
		Upstreams:           group.Upstreams,
		ChannelType:         group.ChannelType,
		Sort:                group.Sort,
		TestModel:           group.TestModel,
		ValidationEndpoint:  group.ValidationEndpoint,
		ParamOverrides:      group.ParamOverrides,
		ModelRedirectRules:  group.ModelRedirectRules,
		ModelRedirectStrict: group.ModelRedirectStrict,
		Config:              group.Config,
		HeaderRules:         headerRules,
		ProxyKeys:           group.ProxyKeys,
		LastValidatedAt:     group.LastValidatedAt,
		CreatedAt:           group.CreatedAt,
		UpdatedAt:           group.UpdatedAt,
	}
}

// DeleteGroup handles deleting a group.
func (s *Server) DeleteGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	if s.handleGroupError(c, s.GroupService.DeleteGroup(c.Request.Context(), uint(id))) {
		return
	}
	response.SuccessI18n(c, "success.group_deleted", nil)
}

// ConfigOption represents a single configurable option for a group.
type ConfigOption struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	DefaultValue any    `json:"default_value"`
}

// GetGroupConfigOptions returns a list of available configuration options for groups.
func (s *Server) GetGroupConfigOptions(c *gin.Context) {
	options, err := s.GroupService.GetGroupConfigOptions()
	if s.handleGroupError(c, err) {
		return
	}

	translated := make([]ConfigOption, 0, len(options))
	for _, option := range options {
		name := option.Name
		if strings.HasPrefix(name, "config.") {
			name = i18n.Message(c, name)
		}
		description := option.Description
		if strings.HasPrefix(description, "config.") {
			description = i18n.Message(c, description)
		}

		translated = append(translated, ConfigOption{
			Key:          option.Key,
			Name:         name,
			Description:  description,
			DefaultValue: option.DefaultValue,
		})
	}

	response.Success(c, translated)
}

// calculateRequestStats is a helper to compute request statistics.
func (s *Server) GetGroupStats(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	stats, err := s.GroupService.GetGroupStats(c.Request.Context(), uint(id))
	if s.handleGroupError(c, err) {
		return
	}

	response.Success(c, stats)
}

// GroupCopyRequest defines the payload for copying a group.
type GroupCopyRequest struct {
	CopyKeys string `json:"copy_keys"` // "none"|"valid_only"|"all"
}

// GroupCopyResponse defines the response for group copy operation.
type GroupCopyResponse struct {
	Group *GroupResponse `json:"group"`
}

// CopyGroup handles copying a group with optional content.

func (s *Server) CopyGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	var req GroupCopyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	newGroup, err := s.GroupService.CopyGroup(c.Request.Context(), uint(id), req.CopyKeys)
	if s.handleGroupError(c, err) {
		return
	}

	groupResponse := s.newGroupResponse(newGroup)
	copyResponse := &GroupCopyResponse{
		Group: groupResponse,
	}

	response.Success(c, copyResponse)
}

// List godoc
func (s *Server) List(c *gin.Context) {
	var groups []models.Group
	if err := s.DB.Select("id, name,display_name").Find(&groups).Error; err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrDatabase, "database.cannot_get_groups")
		return
	}
	response.Success(c, groups)
}

// AddSubGroupsRequest defines the payload for adding sub groups to an aggregate group
type AddSubGroupsRequest struct {
	SubGroups []services.SubGroupInput `json:"sub_groups"`
}

// UpdateSubGroupWeightRequest defines the payload for updating a sub group weight
type UpdateSubGroupWeightRequest struct {
	Weight int `json:"weight"`
}

// GetSubGroups handles getting sub groups of an aggregate group
func (s *Server) GetSubGroups(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	subGroups, err := s.AggregateGroupService.GetSubGroups(c.Request.Context(), uint(id))
	if s.handleGroupError(c, err) {
		return
	}

	response.Success(c, subGroups)
}

// AddSubGroups handles adding sub groups to an aggregate group
func (s *Server) AddSubGroups(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	var req AddSubGroupsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	if err := s.AggregateGroupService.AddSubGroups(c.Request.Context(), uint(id), req.SubGroups); s.handleGroupError(c, err) {
		return
	}

	response.SuccessI18n(c, "success.sub_groups_added", nil)
}

// UpdateSubGroupWeight handles updating the weight of a sub group
func (s *Server) UpdateSubGroupWeight(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	subGroupID, err := strconv.Atoi(c.Param("subGroupId"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_sub_group_id")
		return
	}

	var req UpdateSubGroupWeightRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	if err := s.AggregateGroupService.UpdateSubGroupWeight(c.Request.Context(), uint(id), uint(subGroupID), req.Weight); s.handleGroupError(c, err) {
		return
	}

	response.SuccessI18n(c, "success.sub_group_weight_updated", nil)
}

// DeleteSubGroup handles deleting a sub group from an aggregate group
func (s *Server) DeleteSubGroup(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	subGroupID, err := strconv.Atoi(c.Param("subGroupId"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_sub_group_id")
		return
	}

	if err := s.AggregateGroupService.DeleteSubGroup(c.Request.Context(), uint(id), uint(subGroupID)); s.handleGroupError(c, err) {
		return
	}

	response.SuccessI18n(c, "success.sub_group_deleted", nil)
}

// GetParentAggregateGroups handles getting parent aggregate groups that reference a group
func (s *Server) GetParentAggregateGroups(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrBadRequest, "validation.invalid_group_id")
		return
	}

	parentGroups, err := s.AggregateGroupService.GetParentAggregateGroups(c.Request.Context(), uint(id))
	if s.handleGroupError(c, err) {
		return
	}

	response.Success(c, parentGroups)
}

// GroupUsageData represents the usage data for a group
type GroupUsageData struct {
	GroupID      uint   `json:"group_id"`
	HourlyUsage  int64  `json:"hourly_usage"`
	HourlyLimit  int64  `json:"hourly_limit"`
	MonthlyUsage int64  `json:"monthly_usage"`
	MonthlyLimit int64  `json:"monthly_limit"`
	LastUpdated  string `json:"last_updated"`
}

// GroupMonitorResponse represents the response for group monitor API
type GroupMonitorResponse struct {
	Groups []GroupMonitorItem `json:"groups"`
}

// GroupMonitorItem represents a single group item in the monitor response
type GroupMonitorItem struct {
	*GroupResponse
	UsageData *GroupUsageData `json:"usage_data,omitempty"`
}

// GetGroupMonitor handles the request to get all groups with their usage data
func (s *Server) GetGroupMonitor(c *gin.Context) {
	// Get all groups
	groups, err := s.GroupService.ListGroups(c.Request.Context())
	if s.handleGroupError(c, err) {
		return
	}

	// Get current time boundaries
	now := time.Now()
	currentHour := now.Truncate(time.Hour)
	currentMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	// Prepare result items
	items := make([]GroupMonitorItem, 0, len(groups))

	for i := range groups {
		group := &groups[i]
		groupResp := s.newGroupResponse(group)

		// Get usage data
		usageData := s.getGroupUsageData(group.ID, currentHour, currentMonth)

		// 获取分组的统计信息（24小时、7天和30天）
		stats, err := s.GroupService.GetGroupListStats(c.Request.Context(), group.ID)
		if err == nil && stats != nil {
			groupResp.Stats24Hour = &stats.Stats24Hour
			groupResp.Stats7Day = &stats.Stats7Day
			groupResp.Stats30Day = &stats.Stats30Day
		}

		items = append(items, GroupMonitorItem{
			GroupResponse: groupResp,
			UsageData:    usageData,
		})
	}

	response.Success(c, GroupMonitorResponse{
		Groups: items,
	})
}

// getGroupUsageData retrieves the usage data for a specific group
func (s *Server) getGroupUsageData(groupID uint, currentHour, currentMonth time.Time) *GroupUsageData {
	// Get limits from group config
	var hourlyLimit, monthlyLimit int64 = 0, 0

	var group models.Group
	if err := s.DB.Select("config").Where("id = ?", groupID).First(&group).Error; err == nil {
		if group.Config != nil {
			// Parse config into GroupConfig struct to correctly read the rate limit fields
			var config models.GroupConfig
			configBytes, _ := json.Marshal(group.Config)
			_ = json.Unmarshal(configBytes, &config)

			// Read the rate limit values from the parsed config
			if config.MaxRequestsPerHour != nil && *config.MaxRequestsPerHour > 0 {
				hourlyLimit = int64(*config.MaxRequestsPerHour)
			}
			if config.MaxRequestsPerMonth != nil && *config.MaxRequestsPerMonth > 0 {
				monthlyLimit = int64(*config.MaxRequestsPerMonth)
			}
		}
	}

	// Get hourly usage from GroupHourlyStat (suppress log if not found)
	var hourlyStat models.GroupHourlyStat
	hourlyUsage := int64(0)
	s.DB.Session(&gorm.Session{AllowGlobalUpdate: false}).
		Where("time = ? AND group_id = ?", currentHour, groupID).
		First(&hourlyStat).
		// Only update usage if record exists (ignore ErrRecordNotFound)
		Scan(&hourlyStat)
	if hourlyStat.ID > 0 {
		hourlyUsage = hourlyStat.SuccessCount + hourlyStat.FailureCount
	}

	// Get monthly usage from GroupMonthlyStat (suppress log if not found)
	var monthlyStat models.GroupMonthlyStat
	monthlyUsage := int64(0)
	s.DB.Session(&gorm.Session{AllowGlobalUpdate: false}).
		Where("month = ? AND group_id = ?", currentMonth, groupID).
		First(&monthlyStat).
		// Only update usage if record exists (ignore ErrRecordNotFound)
		Scan(&monthlyStat)
	if monthlyStat.ID > 0 {
		monthlyUsage = monthlyStat.RequestCount
	}

	// If no monthly stat, calculate from hourly stats for the current month
	if monthlyUsage == 0 && monthlyLimit > 0 {
		var sum int64
		s.DB.Model(&models.GroupHourlyStat{}).
			Where("group_id = ? AND time >= ? AND time < ?", groupID, currentMonth, currentMonth.AddDate(0, 1, 0)).
			Select("COALESCE(SUM(success_count + failure_count), 0)").
			Scan(&sum)
		monthlyUsage = sum
	}

	return &GroupUsageData{
		GroupID:      groupID,
		HourlyUsage:  hourlyUsage,
		HourlyLimit:  hourlyLimit,
		MonthlyUsage: monthlyUsage,
		MonthlyLimit: monthlyLimit,
		LastUpdated:  time.Now().Format(time.RFC3339),
	}
}

// GroupSortOrder represents the sort order for groups
type GroupSortOrder struct {
	Order []uint `json:"order"`
}

// GetGroupSortOrder handles getting the group sort order
func (s *Server) GetGroupSortOrder(c *gin.Context) {
	order, err := loadGroupSortOrder()
	if err != nil {
		// 文件不存在时返回空数组
		response.Success(c, []uint{})
		return
	}
	response.Success(c, order)
}

// SaveGroupSortOrder handles saving the group sort order
func (s *Server) SaveGroupSortOrder(c *gin.Context) {
	var req []uint
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrInvalidJSON, err.Error()))
		return
	}

	if err := saveGroupSortOrder(req); err != nil {
		response.ErrorI18nFromAPIError(c, app_errors.ErrInternalServer, "groupMonitor.saveSortFailed")
		return
	}

	response.SuccessI18n(c, "success.sort_order_saved", nil)
}

// getSortOrderFilePath returns the path to the sort order JSON file
func getSortOrderFilePath() string {
	return "group_sort_order.json"
}

// loadGroupSortOrder loads the group sort order from JSON file
func loadGroupSortOrder() ([]uint, error) {
	data, err := os.ReadFile(getSortOrderFilePath())
	if err != nil {
		return nil, err
	}

	var order []uint
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, err
	}

	return order, nil
}

// saveGroupSortOrder saves the group sort order to JSON file
func saveGroupSortOrder(order []uint) error {
	data, err := json.MarshalIndent(order, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(getSortOrderFilePath(), data, 0644)
}
