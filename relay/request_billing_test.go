package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Key-routing retries re-price the request after switching group. The retry path
// must retain the request metadata captured by the initial estimate.
func TestPrepareRequestBillingRetainsPricingMetaForRetry(t *testing.T) {
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousCountToken := constant.CountToken
	constant.CountToken = false
	t.Cleanup(func() {
		constant.CountToken = previousCountToken
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"retry-priced-model":1}`))
	setupRelayChannelDB(t)

	maxTokens := uint(10000)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "retry-priced-model",
		RelayFormat:     types.RelayFormatOpenAI,
		Request:         &dto.GeneralOpenAIRequest{Model: "retry-priced-model", MaxTokens: &maxTokens},
	}

	_ = PrepareRequestBilling(c, info)

	require.NotNil(t, info.BillingRequestMeta)
	assert.Equal(t, int(maxTokens), info.BillingRequestMeta.MaxTokens)
}

type recordingBillingSettler struct {
	preConsumed int
}

func (s *recordingBillingSettler) Settle(int) error         { return nil }
func (s *recordingBillingSettler) Refund(*gin.Context)      {}
func (s *recordingBillingSettler) NeedsRefund() bool        { return false }
func (s *recordingBillingSettler) GetPreConsumedQuota() int { return s.preConsumed }
func (s *recordingBillingSettler) ReserveBeforeSubmit(quota int) error {
	return s.Reserve(quota)
}
func (s *recordingBillingSettler) Reserve(targetQuota int) error {
	if targetQuota > s.preConsumed {
		s.preConsumed = targetQuota
	}
	return nil
}

// RefreshRouteBilling reserves estimated input cost for the selected group.
// A cheaper retry retains the reservation until settlement.
func TestRefreshRouteBillingReservesSelectedGroupInputCost(t *testing.T) {
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	previousMultiplier := operation_setting.GetQuotaSetting().PreConsumeMultiplier
	operation_setting.GetQuotaSetting().PreConsumeMultiplier = 1
	t.Cleanup(func() {
		operation_setting.GetQuotaSetting().PreConsumeMultiplier = previousMultiplier
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"retry-priced-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1,"premium":3}`))

	for _, tc := range []struct {
		name      string
		group     string
		maxTokens int
		reserved  int
		want      int
	}{
		{"estimated input", "default", 0, 0, 7},
		{"large output limit", "default", 10000, 0, 7},
		{"more expensive group", "premium", 10000, 7, 21},
		{"cheaper group", "default", 10000, 21, 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyTokenRoutingConfigured, true)
			common.SetContextKey(c, constant.ContextKeyTokenFailoverEnabled, true)
			settler := &recordingBillingSettler{preConsumed: tc.reserved}
			info := &relaycommon.RelayInfo{
				OriginModelName:    "retry-priced-model",
				UsingGroup:         tc.group,
				Billing:            settler,
				BillingRequestMeta: &types.TokenCountMeta{MaxTokens: tc.maxTokens},
			}
			info.SetEstimatePromptTokens(7)
			require.Nil(t, RefreshRouteBilling(c, info))
			assert.Equal(t, tc.want, settler.preConsumed)
			require.Nil(t, RefreshRouteBilling(c, info))
			assert.Equal(t, tc.want, settler.preConsumed, "re-pricing the same route retains its reservation")
		})
	}
}
