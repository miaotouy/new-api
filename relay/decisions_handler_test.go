package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This fixture observes settlement through the public BillingSettler contract.
type decisionBillingSettler struct {
	reserved int
	actual   int
	settled  bool
}

func (s *decisionBillingSettler) Settle(quota int) error {
	s.actual = quota
	s.settled = true
	return nil
}
func (s *decisionBillingSettler) Refund(*gin.Context)      {}
func (s *decisionBillingSettler) NeedsRefund() bool        { return !s.settled }
func (s *decisionBillingSettler) GetPreConsumedQuota() int { return s.reserved }
func (s *decisionBillingSettler) ReserveBeforeSubmit(quota int) error {
	return s.Reserve(quota)
}
func (s *decisionBillingSettler) Reserve(quota int) error {
	s.reserved = max(s.reserved, quota)
	return nil
}

// Protects provider routing, precision-preserving payload forwarding, ordinary
// settlement and the installed TypeSafe channel's migration onto the host relay.
func TestDecisionsRelayRoutesAndSettles(t *testing.T) {
	settings := config.GlobalConfig.Get("billing_setting").(*billing_setting.BillingSetting)
	savedSettings := *settings
	savedRatios, savedPrices, savedGroups := ratio_setting.ModelRatio2JSONString(), ratio_setting.ModelPrice2JSONString(), ratio_setting.GroupRatio2JSONString()
	*settings = billing_setting.BillingSetting{BillingMode: map[string]string{}, BillingExpr: map[string]string{}, PluginBillingExpr: map[string]string{"typesafe::legacy-priced-model": `tier("custom", u("input_tokens") * 0.084 / 1000000)`}}
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"default":1}`))
	t.Cleanup(func() {
		*settings = savedSettings
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(savedRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(savedGroups))
	})
	database := setupRelayChannelDB(t)
	require.NoError(t, database.AutoMigrate(&model.User{}))
	require.NoError(t, database.Create(&model.User{Id: 1, Username: "decision-user", Quota: 1000000000}).Error)
	logConsume := common.LogConsumeEnabled
	common.LogConsumeEnabled = false
	t.Cleanup(func() { common.LogConsumeEnabled = logConsume })
	for _, tc := range []struct {
		name          string
		channelType   int
		clientModel   string
		upstreamModel string
		path          string
		input         int
		output        string
		wantQuota     int
	}{
		{"openrouter", constant.ChannelTypeOpenRouter, "jev-latest", "jev-1.13.0", "/alpha/decisions", 1000, `,"output_tokens":20`, 21},
		{"legacy typesafe", constant.ChannelTypeTaskPlugin, "jev-latest", "jev-latest", "/v1/systemone", 1000, "", 21},
		{"new api", constant.ChannelTypeNewAPI, "liquid/d1", "liquid/d1", "/v1/decisions", 1000, `,"output_tokens":20`, 10},
		{"direct vendor", constant.ChannelTypeOpenAI, "jev-1.14", "jev-1.14", "/v1/systemone", 1000, `,"output_tokens":20`, 21},
		{"zero usage refund", constant.ChannelTypeOpenRouter, "typesafe/jev-1.13", "typesafe/jev-1.13", "/alpha/decisions", 0, `,"output_tokens":0`, 0},
		{"installed provider override", constant.ChannelTypeTaskPlugin, "legacy-priced-model", "jev-latest", "/v1/systemone", 1000, `,"output_tokens":20`, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type observation struct {
				path          string
				authorization string
				body          []byte
				err           error
			}
			observed := make(chan observation, 1)
			response := fmt.Sprintf(`{"model":"served-model","answers":{"q":{"type":"noul","noul":0.98}},"usage":{"input_tokens":%d%s},"provider_extension":{"accepted":true}}`, tc.input, tc.output)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, err := io.ReadAll(r.Body)
				observed <- observation{r.URL.Path, r.Header.Get("Authorization"), raw, err}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, response)
			}))
			defer server.Close()
			raw := fmt.Sprintf(`{"model":%q,"state":{"number":9007199254740993,"image":{"type":"image_url","image_url":{"url":"https://example.com/test.png"}}},"questions":{"q":{"type":"noul","instructions":"Should this be escalated?"}},"provider":{"order":["TypeSafe"]},"stream":false}`, tc.clientModel)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/systemone", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyChannelType, tc.channelType)
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL+"/v1/")
			common.SetContextKey(c, constant.ContextKeyChannelKey, "upstream-secret")
			common.SetContextKey(c, constant.ContextKeyChannelId, 1)
			common.SetContextKey(c, constant.ContextKeyChannelModelMapping, fmt.Sprintf(`{%q:%q}`, tc.clientModel, tc.upstreamModel))
			common.SetContextKey(c, constant.ContextKeyOriginalModel, tc.clientModel)
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "default")
			if tc.channelType == constant.ChannelTypeTaskPlugin {
				c.Set("task_plugin_key", "typesafe")
			}
			request, err := helper.GetAndValidateDecisionsRequest(c)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(c, types.RelayFormatDecisions, request, nil)
			require.NoError(t, err)
			info.UserId = 1
			info.UserQuota = 1000000000
			info.StartTime = time.Now()
			info.SetEstimatePromptTokens(2000)
			price, err := helper.ModelPriceHelper(c, info, 2000, request.GetTokenCountMeta())
			require.NoError(t, err)
			settler := &decisionBillingSettler{reserved: price.QuotaToPreConsume}
			info.Billing = settler
			info.FinalPreConsumedQuota = price.QuotaToPreConsume
			apiErr := DecisionsHelper(c, info)
			require.Nil(t, apiErr)
			got := <-observed
			require.NoError(t, got.err)
			assert.Equal(t, tc.path, got.path)
			assert.Equal(t, "Bearer upstream-secret", got.authorization)
			var sent map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(got.body, &sent))
			wantModel := tc.upstreamModel
			if tc.channelType == constant.ChannelTypeOpenRouter && tc.upstreamModel == "jev-1.13.0" {
				wantModel = "typesafe/jev-1.13"
			}
			assert.JSONEq(t, fmt.Sprintf("%q", wantModel), string(sent["model"]))
			assert.Contains(t, string(sent["state"]), "9007199254740993")
			assert.JSONEq(t, `{"order":["TypeSafe"]}`, string(sent["provider"]))
			assert.Equal(t, tc.clientModel, request.Model, "retries retain the client model")
			assert.True(t, settler.settled)
			assert.Equal(t, tc.wantQuota, settler.actual)
			if info.TieredBillingSnapshot.TaskUsageBilling {
				other := model.NewLogOther()
				service.InjectTieredBillingInfo(other, info, nil)
				var logged map[string]any
				require.NoError(t, common.Unmarshal([]byte(other.JSONString()), &logged))
				require.Contains(t, logged, "usage_facts")
				assert.Equal(t, float64(tc.input), logged["usage_facts"].(map[string]any)["input_tokens"])
			}
			assert.Equal(t, http.StatusOK, recorder.Code)
			assert.JSONEq(t, response, recorder.Body.String())
		})
	}
}

// Malformed or absent upstream usage must not become a free inference; genuine
// zeros remain authoritative. TypeSafe's input-only usage stays compatible.
func TestDecisionResponseUsage(t *testing.T) {
	request := &dto.DecisionsRequest{Questions: map[string]dto.DecisionQuestion{"q": {Type: "choice"}}}
	for _, tc := range []struct {
		name       string
		usage      string
		wantInput  int
		wantOutput int
		estimated  bool
	}{
		{"measured", `{"input_tokens":100000,"output_tokens":17}`, 100000, 17, false},
		{"input only", `{"input_tokens":296}`, 296, 0, false},
		{"explicit zero", `{"input_tokens":0,"output_tokens":0}`, 0, 0, false},
		{"missing", `null`, 300, 0, true},
		{"negative", `{"input_tokens":-1,"output_tokens":0}`, 300, 0, true},
		{"fractional", `{"input_tokens":0.5,"output_tokens":0}`, 300, 0, true},
		{"overflow", `{"input_tokens":18446744073709551615,"output_tokens":0}`, 300, 0, true},
		{"oversized", `{"input_tokens":2147483648,"output_tokens":0}`, 300, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"model":"m","answers":{"q":{"type":"choice","choice":"yes"}},"usage":%s}`, tc.usage))
			usage, estimated, err := decisionResponseUsage(raw, request, 300)
			require.NoError(t, err)
			assert.Equal(t, tc.wantInput, usage.PromptTokens)
			assert.Equal(t, tc.wantOutput, usage.CompletionTokens)
			assert.Equal(t, tc.estimated, estimated)
		})
	}
	_, _, err := decisionResponseUsage([]byte(`{"model":"m","answers":{"q":{"type":"noul"}},"usage":{"input_tokens":100}}`), request, 300)
	require.Error(t, err, "mismatched answers cannot be presented as a successful decision")
}

func TestDecisionsRequestValidation(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		name      string
		question  string
		extra     string
		wantError bool
	}{
		{"choice", `{"type":"choice","criteria":{"yes":"Match","no":null}}`, "", false},
		{"score", `{"type":"score","criteria":["Low","High"]}`, "", false},
		{"noul", `{"type":"noul","criteria":{"true":"Match","false":"Other"}}`, "", false},
		{"multimodal box", `{"type":"box","instructions":{"image_index":0,"target":"cat"}}`, "", false},
		{"empty choice", `{"type":"choice","criteria":{}}`, "", true},
		{"wrong criterion", `{"type":"score","criteria":[false]}`, "", true},
		{"invalid noul", `{"type":"noul","criteria":{"maybe":"Match"}}`, "", true},
		{"unsupported stream", `{"type":"noul"}`, `,"stream":true`, true},
		{"token overflow", `{"type":"noul"}`, `,"max_tokens":18446744073709551615`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"model":"any/new-model","state":[{"type":"image_url","image_url":{"url":"https://example.com/a.png"}}],"questions":{"q":%s}%s}`, tc.question, tc.extra)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			_, err := helper.GetAndValidateDecisionsRequest(c)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// Disabling optional tokenization must still reserve a nonzero decision input,
// and inputs larger than the retired 64K plugin budget remain usable.
func TestDecisionsReservationWithoutTokenizer(t *testing.T) {
	countTokens := constant.CountToken
	previousGroups := ratio_setting.GroupRatio2JSONString()
	constant.CountToken = false
	previousFreeReservation := operation_setting.GetQuotaSetting().EnableFreeModelPreConsume
	operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = false
	t.Cleanup(func() { operation_setting.GetQuotaSetting().EnableFreeModelPreConsume = previousFreeReservation })
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"decision-free":0}`))
	t.Cleanup(func() {
		constant.CountToken = countTokens
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(previousGroups))
	})

	for _, modelName := range []string{"typesafe/jev-1.14", "legacy-alias"} {
		t.Run(modelName, func(t *testing.T) {
			raw := fmt.Sprintf(`{"model":%q,"state":"%s","questions":{"q":{"type":"noul"}}}`, modelName, strings.Repeat("x", 70000))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/decisions", strings.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			common.SetContextKey(c, constant.ContextKeyUsingGroup, "decision-free")
			common.SetContextKey(c, constant.ContextKeyOriginalModel, modelName)
			if modelName == "legacy-alias" {
				common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeTaskPlugin)
				common.SetContextKey(c, constant.ContextKeyChannelModelMapping, `{"legacy-alias":"jev-latest"}`)
				c.Set("task_plugin_key", "typesafe")
			}
			request, err := helper.GetAndValidateDecisionsRequest(c)
			require.NoError(t, err)
			info, err := relaycommon.GenRelayInfo(c, types.RelayFormatDecisions, request, nil)
			require.NoError(t, err)
			require.Nil(t, PrepareRequestBilling(c, info))
			assert.Equal(t, len(raw), info.GetEstimatePromptTokens())
			require.NotNil(t, info.TieredBillingSnapshot)
			assert.Greater(t, info.TieredBillingSnapshot.EstimatedPromptTokens, 65536)
			if modelName == "legacy-alias" {
				assert.Equal(t, "jev-latest", info.GetBillingModelName())
			}
		})
	}
}
