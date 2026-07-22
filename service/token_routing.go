package service

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

type RouteCandidate struct {
	Group         string
	ChannelID     int
	Priority      int64
	Weight        uint
	GroupRatio    float64
	EstimatedCost float64
}

type RoutePlan struct {
	Candidates      []RouteCandidate
	Mode            string
	Strategy        string
	MaxRatio        float64
	FailoverEnabled bool
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
	if len(plan.Candidates) == 0 {
		return nil, tokenGroup, fmt.Errorf("没有符合密钥路由策略的可用渠道")
	}
	common.SetContextKey(c, constant.ContextKeyTokenRoutePlan, plan)
	for _, candidate := range plan.Candidates {
		channel, err := model.CacheGetChannel(candidate.ChannelID)
		if err != nil {
			continue
		}
		if channel != nil && channel.Status == common.ChannelStatusEnabled {
			return channel, candidate.Group, nil
		}
	}
	return nil, tokenGroup, fmt.Errorf("没有符合密钥路由策略的启用渠道")
}

func SelectNextTokenRouteChannel(c *gin.Context, retry int) (*model.Channel, string, error) {
	value, ok := common.GetContextKey(c, constant.ContextKeyTokenRoutePlan)
	if !ok {
		return nil, "", fmt.Errorf("密钥路由计划不存在")
	}
	plan, ok := value.(RoutePlan)
	if !ok {
		return nil, "", fmt.Errorf("密钥路由计划类型无效")
	}
	if retry > 0 && !plan.FailoverEnabled {
		return nil, "", fmt.Errorf("密钥未启用故障转移")
	}
	if retry < 0 || retry >= len(plan.Candidates) {
		return nil, "", nil
	}
	for index := retry; index < len(plan.Candidates); index++ {
		candidate := plan.Candidates[index]
		channel, err := model.CacheGetChannel(candidate.ChannelID)
		if err != nil || channel == nil || channel.Status != common.ChannelStatusEnabled {
			continue
		}
		return channel, candidate.Group, nil
	}
	return nil, "", nil
}

func BuildTokenRoutePlan(c *gin.Context, tokenGroup, modelName, requestPath string) (RoutePlan, error) {
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
	plan := RoutePlan{Mode: mode, Strategy: strategy, MaxRatio: maxRatio, FailoverEnabled: failover}

	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	if mode == "manual" {
		return buildManualRoutePlan(c, &plan, modelName, requestPath, userGroup)
	}
	return buildAutomaticRoutePlan(c, &plan, tokenGroup, modelName, requestPath, userGroup)
}

func buildManualRoutePlan(c *gin.Context, plan *RoutePlan, modelName, requestPath, userGroup string) (RoutePlan, error) {
	tokenID := common.GetContextKeyInt(c, constant.ContextKeyTokenId)
	rules, err := model.GetTokenRouteRules(tokenID)
	if err != nil {
		return *plan, err
	}
	seen := make(map[int]struct{})
	for _, rule := range rules {
		if rule.Kind == model.TokenRouteKindGroup {
			if !routeGroupAllowed(userGroup, rule.GroupName, plan.MaxRatio) {
				continue
			}
			channels, err := model.GetSatisfiedChannels(rule.GroupName, modelName, requestPath)
			if err != nil {
				return *plan, err
			}
			appendRouteChannels(plan, seen, rule.GroupName, channels, userGroup)
			continue
		}
		if rule.Kind != model.TokenRouteKindChannel || rule.ChannelID <= 0 || !routeGroupAllowed(userGroup, rule.GroupName, plan.MaxRatio) {
			continue
		}
		channels, err := model.GetSatisfiedChannels(rule.GroupName, modelName, requestPath)
		if err != nil {
			return *plan, err
		}
		for _, channel := range channels {
			if channel.Id == rule.ChannelID {
				appendRouteCandidate(plan, seen, rule.GroupName, channel, userGroup)
				break
			}
		}
	}
	if len(plan.Candidates) == 0 {
		return *plan, fmt.Errorf("手动路由没有符合条件的可用渠道")
	}
	return *plan, nil
}

func buildAutomaticRoutePlan(c *gin.Context, plan *RoutePlan, tokenGroup, modelName, requestPath, userGroup string) (RoutePlan, error) {
	groups := routeGroups(userGroup, tokenGroup, plan.FailoverEnabled)
	seenGroups := make(map[string]struct{}, len(groups))
	seenChannels := make(map[int]struct{})
	for _, group := range groups {
		if _, ok := seenGroups[group]; ok || !routeGroupAllowed(userGroup, group, plan.MaxRatio) {
			continue
		}
		seenGroups[group] = struct{}{}
		channels, err := model.GetSatisfiedChannels(group, modelName, requestPath)
		if err != nil {
			return *plan, err
		}
		appendRouteChannels(plan, seenChannels, group, channels, userGroup)
		if !plan.FailoverEnabled && len(plan.Candidates) > 0 {
			break
		}
	}
	if plan.Strategy == "price" {
		sort.SliceStable(plan.Candidates, func(i, j int) bool {
			left, right := plan.Candidates[i], plan.Candidates[j]
			if left.EstimatedCost != right.EstimatedCost {
				return left.EstimatedCost < right.EstimatedCost
			}
			if left.Priority != right.Priority {
				return left.Priority > right.Priority
			}
			if left.Weight != right.Weight {
				return left.Weight > right.Weight
			}
			return left.ChannelID < right.ChannelID
		})
	}
	if len(plan.Candidates) == 0 {
		return *plan, fmt.Errorf("没有符合密钥路由策略的可用渠道")
	}
	return *plan, nil
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
		if _, ok := seen[group]; !ok {
			seen[group] = struct{}{}
			groups = append(groups, group)
		}
	}
	for group := range GetUserUsableGroups(userGroup) {
		if group == "auto" {
			continue
		}
		if _, ok := seen[group]; !ok && ratio_setting.ContainsGroupRatio(group) {
			seen[group] = struct{}{}
			groups = append(groups, group)
		}
	}
	sort.Strings(groups[1:])
	return groups
}

func routeGroupAllowed(userGroup, group string, maxRatio float64) bool {
	if group == "" || group == "auto" || !GroupInUserUsableGroups(userGroup, group) || !ratio_setting.ContainsGroupRatio(group) {
		return false
	}
	ratio := GetUserGroupRatio(userGroup, group)
	return maxRatio <= 0 || ratio <= maxRatio
}

func appendRouteChannels(plan *RoutePlan, seen map[int]struct{}, group string, channels []*model.Channel, userGroup string) {
	for _, channel := range channels {
		appendRouteCandidate(plan, seen, group, channel, userGroup)
	}
}

func appendRouteCandidate(plan *RoutePlan, seen map[int]struct{}, group string, channel *model.Channel, userGroup string) {
	if channel == nil {
		return
	}
	if _, ok := seen[channel.Id]; ok {
		return
	}
	seen[channel.Id] = struct{}{}
	ratio := GetUserGroupRatio(userGroup, group)
	plan.Candidates = append(plan.Candidates, RouteCandidate{
		Group:         group,
		ChannelID:     channel.Id,
		Priority:      channel.GetPriority(),
		Weight:        uint(channel.GetWeight()),
		GroupRatio:    ratio,
		EstimatedCost: ratio,
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

func routeModeFromContext(c *gin.Context) string {
	return strings.TrimSpace(common.GetContextKeyString(c, constant.ContextKeyTokenRouteMode))
}
