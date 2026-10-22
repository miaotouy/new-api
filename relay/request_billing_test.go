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
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Key-routing retries re-price the request after switching group. The retry path
// must reuse the exact metadata of the initial estimate, otherwise the output
// token budget (max_tokens) is lost and the second reservation undercharges.
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

// RefreshRouteBilling re-prices an existing reservation for the selected route
// group. It must include the retained output-token budget, otherwise retry
// switching to a more expensive group reserves too little.
func TestRefreshRouteBillingReservesWithRetainedMaxTokens(t *testing.T) {
	previousRatios := ratio_setting.ModelRatio2JSONString()
	previousGroupRatios := ratio_setting.GroupRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(previousRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroupRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"retry-priced-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))

	reserveFor := func(maxTokens int) int {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		common.SetContextKey(c, constant.ContextKeyTokenRoutingConfigured, true)
		common.SetContextKey(c, constant.ContextKeyTokenFailoverEnabled, true)
		settler := &recordingBillingSettler{}
		info := &relaycommon.RelayInfo{
			OriginModelName:    "retry-priced-model",
			UsingGroup:         "default",
			Billing:            settler,
			BillingRequestMeta: &types.TokenCountMeta{MaxTokens: maxTokens},
		}
		info.SetEstimatePromptTokens(7)
		require.Nil(t, RefreshRouteBilling(c, info))
		return settler.preConsumed
	}

	assert.Greater(t, reserveFor(10000), reserveFor(0), "the retained max_tokens budget must affect the reservation")
}
