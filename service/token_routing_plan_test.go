package service

import (
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPriorityRouteCandidateUsesWeightWithinPriority(t *testing.T) {
	plan := &RoutePlan{
		Candidates: []RouteCandidate{
			{ChannelID: 1, Priority: 10, Weight: 0, scopePosition: 0},
			{ChannelID: 2, Priority: 10, Weight: 100, scopePosition: 0},
			{ChannelID: 3, Priority: 9, Weight: 100, scopePosition: 0},
		},
		consumedChannelIDs: map[int]struct{}{},
	}

	candidate, found := nextRouteCandidate(plan)

	require.True(t, found)
	require.Equal(t, 2, candidate.ChannelID)
}

func TestRouteCandidateDoesNotRepeatConsumedChannel(t *testing.T) {
	plan := &RoutePlan{
		Candidates: []RouteCandidate{
			{ChannelID: 1, Priority: 10, Weight: 1, scopePosition: 0},
			{ChannelID: 2, Priority: 10, Weight: 1, scopePosition: 0},
		},
		consumedChannelIDs: map[int]struct{}{1: {}},
	}

	candidate, found := nextRouteCandidate(plan)

	require.True(t, found)
	require.Equal(t, 2, candidate.ChannelID)
}

func TestPriceRouteCandidatesPreferLowerGroupRatio(t *testing.T) {
	candidates := []RouteCandidate{
		{ChannelID: 1, GroupRatio: 2, Priority: 100, ResponseTime: 1, Weight: 100},
		{ChannelID: 2, GroupRatio: 1, Priority: 1, ResponseTime: 100, Weight: 1},
	}

	sortRouteCandidatesByPrice(candidates)

	require.Equal(t, []int{2, 1}, []int{candidates[0].ChannelID, candidates[1].ChannelID})
}

func TestPriceRouteCandidatesUseResponseTimeBeforeWeight(t *testing.T) {
	candidates := []RouteCandidate{
		{ChannelID: 1, GroupRatio: 1, Priority: 10, ResponseTime: 50, Weight: 100},
		{ChannelID: 2, GroupRatio: 1, Priority: 10, ResponseTime: 10, Weight: 1},
		{ChannelID: 3, GroupRatio: 1, Priority: 9, ResponseTime: 1, Weight: 100},
	}

	sortRouteCandidatesByPrice(candidates)

	require.Equal(t, []int{2, 1, 3}, []int{candidates[0].ChannelID, candidates[1].ChannelID, candidates[2].ChannelID})
}

func TestRouteGroupsPreserveAutoGroupOrderBeforeRatioSortedFallbacks(t *testing.T) {
	previousAutoGroups := setting.AutoGroups2JsonString()
	previousUsableGroups := setting.UserUsableGroups2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateAutoGroupsByJsonString(previousAutoGroups))
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousUsableGroups))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})

	require.NoError(t, setting.UpdateAutoGroupsByJsonString(`["vip","default"]`))
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"fixed":"","vip":"","default":"","standard":"","premium":"","alpha":"","beta":""}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"fixed":1,"vip":2,"default":1,"standard":1.5,"premium":1.2,"alpha":1.5,"beta":1.5}`))

	require.Equal(t,
		[]string{"fixed", "vip", "default", "premium", "alpha", "beta", "standard"},
		routeGroups("", "fixed", true),
	)
}

func TestTokenRouteAttemptIsAuditedWithFallbackReason(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	plan := &RoutePlan{
		Mode:               "manual",
		Strategy:           "priority",
		MaxRatio:           2,
		FailoverEnabled:    true,
		consumedChannelIDs: map[int]struct{}{},
	}
	consumeRouteCandidate(plan, RouteCandidate{Group: "group-b", ChannelID: 42}, "")
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutingConfigured, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenRouteMode, "manual")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoRouteStrategy, "priority")
	common.SetContextKey(ctx, constant.ContextKeyTokenMaxRatio, 2.0)
	common.SetContextKey(ctx, constant.ContextKeyTokenFailoverEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutePlan, plan)

	RecordTokenRouteFallback(ctx, types.NewErrorWithStatusCode(errors.New("upstream unavailable"), "", 503))
	adminInfo := make(map[string]interface{})
	AppendTokenRouteAdminInfo(ctx, adminInfo)

	attempts, ok := adminInfo["route_attempts"].([]RouteAttempt)
	require.True(t, ok)
	require.Len(t, attempts, 1)
	require.Equal(t, "upstream_5xx", attempts[0].FallbackReason)
	require.Equal(t, "group-b", adminInfo["final_route_group"])
	require.Equal(t, 42, adminInfo["final_route_channel_id"])
}
