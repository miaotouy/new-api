package service

import (
	"fmt"
	"math/rand"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type RouteCandidate struct {
	Group         string
	ChannelID     int
	Priority      int64
	Weight        uint
	ResponseTime  int
	GroupRatio    float64
	scopePosition int
}

type RouteAttempt struct {
	RouteMode          string `json:"route_mode"`
	CandidateGroup     string `json:"candidate_group"`
	CandidateChannelID int    `json:"candidate_channel_id"`
	FallbackReason     string `json:"fallback_reason,omitempty"`
	AttemptIndex       int    `json:"attempt_index"`
}

type RoutePlan struct {
	Candidates      []RouteCandidate
	Mode            string
	Strategy        string
	MaxRatio        float64
	FailoverEnabled bool
	Attempts        []RouteAttempt

	preparedCandidateID int
	consumedChannelIDs  map[int]struct{}
}

func ShouldUseTokenRouting(c *gin.Context) bool {
	if c == nil {
		return false
	}
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenRoutingConfigured) {
		return false
	}
	mode := common.GetContextKeyString(c, constant.ContextKeyTokenRouteMode)
	strategy := common.GetContextKeyString(c, constant.ContextKeyTokenAutoRouteStrategy)
	maxRatio, _ := tokenContextFloat(c, constant.ContextKeyTokenMaxRatio)
	return mode == "manual" || strategy == "price" || maxRatio > 0 || common.GetContextKeyBool(c, constant.ContextKeyTokenFailoverEnabled)
}

func ShouldSkipTokenRouteRetry(c *gin.Context) bool {
	if !ShouldUseTokenRouting(c) {
		return false
	}
	if common.GetContextKeyBool(c, constant.ContextKeyTokenFailoverEnabled) {
		return false
	}
	mode := common.GetContextKeyString(c, constant.ContextKeyTokenRouteMode)
	return mode == "manual" || common.GetContextKeyString(c, constant.ContextKeyTokenAutoRouteStrategy) == "price"
}

func SelectTokenRouteChannel(c *gin.Context, tokenGroup, modelName, requestPath string) (*model.Channel, string, error) {
	plan, err := BuildTokenRoutePlan(c, tokenGroup, modelName, requestPath)
	if err != nil {
		return nil, tokenGroup, err
	}
	channel, candidate, err := selectRouteCandidate(plan, false)
	if err != nil {
		return nil, tokenGroup, err
	}
	plan.preparedCandidateID = candidate.ChannelID
	common.SetContextKey(c, constant.ContextKeyTokenRoutePlan, plan)
	return channel, candidate.Group, nil
}

func SelectNextTokenRouteChannel(c *gin.Context, retry int) (*model.Channel, string, error) {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutePlan)
	if !ok {
		return nil, "", fmt.Errorf("密钥路由计划不存在")
	}
	plan, ok := value.(*RoutePlan)
	if !ok || plan == nil {
		return nil, "", fmt.Errorf("密钥路由计划类型无效")
	}
	if retry > 0 && !plan.FailoverEnabled {
		return nil, "", fmt.Errorf("密钥未启用故障转移")
	}

	if retry == 0 && plan.preparedCandidateID > 0 {
		candidate, found := routeCandidateByChannelID(plan, plan.preparedCandidateID)
		plan.preparedCandidateID = 0
		if found {
			channel, err := model.CacheGetChannel(candidate.ChannelID)
			if err == nil && channel != nil && channel.Status == common.ChannelStatusEnabled {
				consumeRouteCandidate(plan, candidate, "")
				return channel, candidate.Group, nil
			}
			consumeRouteCandidate(plan, candidate, "channel_unavailable")
		}
	}

	channel, candidate, err := selectRouteCandidate(plan, true)
	if err != nil {
		return nil, "", err
	}
	return channel, candidate.Group, nil
}

func BuildTokenRoutePlan(c *gin.Context, tokenGroup, modelName, requestPath string) (*RoutePlan, error) {
	mode := common.GetContextKeyString(c, constant.ContextKeyTokenRouteMode)
	if mode == "" {
		mode = "auto"
	}
	strategy := common.GetContextKeyString(c, constant.ContextKeyTokenAutoRouteStrategy)
	if strategy == "" {
		strategy = "priority"
	}
	maxRatio, _ := tokenContextFloat(c, constant.ContextKeyTokenMaxRatio)
	failover := common.GetContextKeyBool(c, constant.ContextKeyTokenFailoverEnabled)
	plan := &RoutePlan{
		Mode:               mode,
		Strategy:           strategy,
		MaxRatio:           maxRatio,
		FailoverEnabled:    failover,
		consumedChannelIDs: make(map[int]struct{}),
	}

	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if mode == "manual" {
		return buildManualRoutePlan(c, plan, modelName, requestPath, userGroup)
	}
	return buildAutomaticRoutePlan(c, plan, tokenGroup, modelName, requestPath, userGroup)
}

func buildManualRoutePlan(c *gin.Context, plan *RoutePlan, modelName, requestPath, userGroup string) (*RoutePlan, error) {
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	rules, err := model.GetTokenRouteRules(tokenID)
	if err != nil {
		return plan, err
	}
	seen := make(map[int]struct{})
	for _, rule := range rules {
		if rule.Kind == model.TokenRouteKindGroup {
			if !routeGroupAllowed(userGroup, rule.GroupName, plan.MaxRatio) {
				continue
			}
			channels, err := model.GetSatisfiedChannels(rule.GroupName, modelName, requestPath)
			if err != nil {
				return plan, err
			}
			appendRouteChannels(plan, seen, rule.GroupName, channels, userGroup, rule.Position)
			continue
		}
		if rule.Kind != model.TokenRouteKindChannel || rule.ChannelID <= 0 || !routeGroupAllowed(userGroup, rule.GroupName, plan.MaxRatio) {
			continue
		}
		channels, err := model.GetSatisfiedChannels(rule.GroupName, modelName, requestPath)
		if err != nil {
			return plan, err
		}
		for _, channel := range channels {
			if channel.Id == rule.ChannelID {
				appendRouteCandidate(plan, seen, rule.GroupName, channel, userGroup, rule.Position)
				break
			}
		}
	}
	if len(plan.Candidates) == 0 {
		return plan, fmt.Errorf("手动路由没有符合条件的可用渠道")
	}
	return plan, nil
}

func buildAutomaticRoutePlan(c *gin.Context, plan *RoutePlan, tokenGroup, modelName, requestPath, userGroup string) (*RoutePlan, error) {
	groups := routeGroups(userGroup, tokenGroup, plan.FailoverEnabled)
	seenGroups := make(map[string]struct{}, len(groups))
	seenChannels := make(map[int]struct{})
	for index, group := range groups {
		if _, ok := seenGroups[group]; ok || !routeGroupAllowed(userGroup, group, plan.MaxRatio) {
			continue
		}
		seenGroups[group] = struct{}{}
		channels, err := model.GetSatisfiedChannels(group, modelName, requestPath)
		if err != nil {
			return plan, err
		}
		appendRouteChannels(plan, seenChannels, group, channels, userGroup, index)
		if !plan.FailoverEnabled && len(plan.Candidates) > 0 {
			break
		}
	}
	if plan.Strategy == "price" {
		sortRouteCandidatesByPrice(plan.Candidates)
	}
	if len(plan.Candidates) == 0 {
		return plan, fmt.Errorf("没有符合密钥路由策略的可用渠道")
	}
	return plan, nil
}

func sortRouteCandidatesByPrice(candidates []RouteCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.GroupRatio != right.GroupRatio {
			return left.GroupRatio < right.GroupRatio
		}
		if left.Priority != right.Priority {
			return left.Priority > right.Priority
		}
		if left.ResponseTime != right.ResponseTime {
			return left.ResponseTime < right.ResponseTime
		}
		if left.Weight != right.Weight {
			return left.Weight > right.Weight
		}
		return left.ChannelID < right.ChannelID
	})
}

func selectRouteCandidate(plan *RoutePlan, consume bool) (*model.Channel, RouteCandidate, error) {
	if plan == nil {
		return nil, RouteCandidate{}, fmt.Errorf("密钥路由计划不存在")
	}
	for {
		candidate, found := nextRouteCandidate(plan)
		if !found {
			return nil, RouteCandidate{}, fmt.Errorf("密钥路由候选已耗尽")
		}
		channel, err := model.CacheGetChannel(candidate.ChannelID)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			consumeRouteCandidate(plan, candidate, "channel_unavailable")
			continue
		}
		if consume {
			consumeRouteCandidate(plan, candidate, "")
		}
		return channel, candidate, nil
	}
}

func nextRouteCandidate(plan *RoutePlan) (RouteCandidate, bool) {
	available := make([]RouteCandidate, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		if _, used := plan.consumedChannelIDs[candidate.ChannelID]; !used {
			available = append(available, candidate)
		}
	}
	if len(available) == 0 {
		return RouteCandidate{}, false
	}
	if plan.Strategy == "price" {
		return available[0], true
	}

	scope := available[0].scopePosition
	for _, candidate := range available[1:] {
		if candidate.scopePosition < scope {
			scope = candidate.scopePosition
		}
	}
	priority := int64(-1 << 63)
	for _, candidate := range available {
		if candidate.scopePosition == scope && candidate.Priority > priority {
			priority = candidate.Priority
		}
	}
	weighted := make([]RouteCandidate, 0, len(available))
	for _, candidate := range available {
		if candidate.scopePosition == scope && candidate.Priority == priority {
			weighted = append(weighted, candidate)
		}
	}
	return selectWeightedRouteCandidate(weighted), true
}

func selectWeightedRouteCandidate(candidates []RouteCandidate) RouteCandidate {
	if len(candidates) == 1 {
		return candidates[0]
	}
	totalWeight := 0
	for _, candidate := range candidates {
		totalWeight += int(candidate.Weight)
	}
	if totalWeight == 0 {
		return candidates[rand.Intn(len(candidates))]
	}
	randomWeight := rand.Intn(totalWeight)
	for _, candidate := range candidates {
		randomWeight -= int(candidate.Weight)
		if randomWeight < 0 {
			return candidate
		}
	}
	return candidates[len(candidates)-1]
}

func consumeRouteCandidate(plan *RoutePlan, candidate RouteCandidate, fallbackReason string) {
	plan.consumedChannelIDs[candidate.ChannelID] = struct{}{}
	plan.Attempts = append(plan.Attempts, RouteAttempt{
		RouteMode:          plan.Mode,
		CandidateGroup:     candidate.Group,
		CandidateChannelID: candidate.ChannelID,
		FallbackReason:     fallbackReason,
		AttemptIndex:       len(plan.Attempts),
	})
}

func routeCandidateByChannelID(plan *RoutePlan, channelID int) (RouteCandidate, bool) {
	for _, candidate := range plan.Candidates {
		if candidate.ChannelID == channelID {
			return candidate, true
		}
	}
	return RouteCandidate{}, false
}

func RecordTokenRouteFallback(c *gin.Context, err *types.NewAPIError) {
	if c == nil || err == nil {
		return
	}
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutePlan)
	if !ok {
		return
	}
	plan, ok := value.(*RoutePlan)
	if !ok || plan == nil || len(plan.Attempts) == 0 {
		return
	}
	last := &plan.Attempts[len(plan.Attempts)-1]
	if last.FallbackReason != "" {
		return
	}
	if types.IsChannelError(err) {
		last.FallbackReason = "channel_error"
		return
	}
	if types.IsSkipRetryError(err) {
		last.FallbackReason = "skip_retry"
		return
	}
	if err.StatusCode >= 500 && err.StatusCode <= 599 {
		last.FallbackReason = "upstream_5xx"
		return
	}
	if err.StatusCode >= 400 && err.StatusCode <= 499 {
		last.FallbackReason = "client_4xx"
		return
	}
	last.FallbackReason = "network_error"
}

func AppendTokenRouteAdminInfo(c *gin.Context, adminInfo map[string]interface{}) {
	if c == nil || adminInfo == nil || !ShouldUseTokenRouting(c) {
		return
	}
	adminInfo["route_mode"] = common.GetContextKeyString(c, constant.ContextKeyTokenRouteMode)
	adminInfo["auto_route_strategy"] = common.GetContextKeyString(c, constant.ContextKeyTokenAutoRouteStrategy)
	if maxRatio, ok := common.GetContextKey(c, constant.ContextKeyTokenMaxRatio); ok {
		adminInfo["max_ratio"] = maxRatio
	}
	adminInfo["failover_enabled"] = common.GetContextKeyBool(c, constant.ContextKeyTokenFailoverEnabled)
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutePlan)
	if !ok {
		return
	}
	plan, ok := value.(*RoutePlan)
	if !ok || plan == nil {
		return
	}
	adminInfo["route_attempts"] = plan.Attempts
	if len(plan.Attempts) > 0 {
		last := plan.Attempts[len(plan.Attempts)-1]
		adminInfo["final_route_group"] = last.CandidateGroup
		adminInfo["final_route_channel_id"] = last.CandidateChannelID
	}
}

func routeGroups(userGroup, tokenGroup string, failover bool) []string {
	if tokenGroup == "auto" {
		return GetUserAutoGroup(userGroup)
	}
	groups := []string{tokenGroup}
	if !failover {
		return groups
	}
	seen := map[string]struct{}{tokenGroup: {}}
	for _, group := range GetUserAutoGroup(userGroup) {
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		groups = append(groups, group)
	}

	remainingGroups := make([]string, 0)
	for group := range GetUserUsableGroups(userGroup) {
		if group == "auto" || !ratio_setting.ContainsGroupRatio(group) {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		remainingGroups = append(remainingGroups, group)
	}
	sort.Slice(remainingGroups, func(i, j int) bool {
		leftRatio := GetUserGroupRatio(userGroup, remainingGroups[i])
		rightRatio := GetUserGroupRatio(userGroup, remainingGroups[j])
		if leftRatio != rightRatio {
			return leftRatio < rightRatio
		}
		return remainingGroups[i] < remainingGroups[j]
	})
	return append(groups, remainingGroups...)
}

func routeGroupAllowed(userGroup, group string, maxRatio float64) bool {
	if group == "" || group == "auto" || !GroupInUserUsableGroups(userGroup, group) || !ratio_setting.ContainsGroupRatio(group) {
		return false
	}
	ratio := GetUserGroupRatio(userGroup, group)
	return maxRatio <= 0 || ratio <= maxRatio
}

func appendRouteChannels(plan *RoutePlan, seen map[int]struct{}, group string, channels []*model.Channel, userGroup string, scopePosition int) {
	for _, channel := range channels {
		appendRouteCandidate(plan, seen, group, channel, userGroup, scopePosition)
	}
}

func appendRouteCandidate(plan *RoutePlan, seen map[int]struct{}, group string, channel *model.Channel, userGroup string, scopePosition int) {
	if channel == nil {
		return
	}
	if _, ok := seen[channel.Id]; ok {
		return
	}
	seen[channel.Id] = struct{}{}
	plan.Candidates = append(plan.Candidates, RouteCandidate{
		Group:         group,
		ChannelID:     channel.Id,
		Priority:      channel.GetPriority(),
		Weight:        uint(channel.GetWeight()),
		ResponseTime:  channel.ResponseTime,
		GroupRatio:    GetUserGroupRatio(userGroup, group),
		scopePosition: scopePosition,
	})
}

func tokenContextFloat(c *gin.Context, key constant.ContextKey) (float64, bool) {
	value, ok := common.GetContextKey(c, key)
	if !ok {
		return 0, false
	}
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	default:
		return 0, false
	}
}
