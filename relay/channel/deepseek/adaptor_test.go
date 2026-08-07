package deepseek

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRequestURLUsesNativeResponsesEndpoint(t *testing.T) {
	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeResponses,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.deepseek.com",
		},
	}

	requestURL, err := adaptor.GetRequestURL(info)

	require.NoError(t, err)
	assert.Equal(t, "https://api.deepseek.com/responses", requestURL)
}

func TestConvertOpenAIResponsesRequestAppliesDeepSeekV4ReasoningSuffix(t *testing.T) {
	tests := []struct {
		name           string
		requestModel   string
		upstreamModel  string
		existingReason *dto.Reasoning
		wantModel      string
		wantEffort     string
		wantSummary    string
	}{
		{
			name:          "none suffix disables reasoning",
			requestModel:  "deepseek-v4-flash-none",
			upstreamModel: "deepseek-v4-flash-none",
			wantModel:     "deepseek-v4-flash",
			wantEffort:    "none",
		},
		{
			name:           "max suffix preserves other reasoning fields",
			requestModel:   "deepseek-v4-flash-max",
			upstreamModel:  "deepseek-v4-flash-max",
			existingReason: &dto.Reasoning{Summary: "auto"},
			wantModel:      "deepseek-v4-flash",
			wantEffort:     "max",
			wantSummary:    "auto",
		},
		{
			name:           "plain model preserves client effort",
			requestModel:   "deepseek-v4-flash",
			upstreamModel:  "deepseek-v4-flash",
			existingReason: &dto.Reasoning{Effort: "high"},
			wantModel:      "deepseek-v4-flash",
			wantEffort:     "high",
		},
		{
			name:          "mapped upstream suffix controls request",
			requestModel:  "deepseek-coding",
			upstreamModel: "deepseek-v4-flash-max",
			wantModel:     "deepseek-v4-flash",
			wantEffort:    "max",
		},
	}

	adaptor := &Adaptor{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				RelayFormat: types.RelayFormatOpenAIResponses,
				ChannelMeta: &relaycommon.ChannelMeta{
					UpstreamModelName: tt.upstreamModel,
				},
			}
			request := dto.OpenAIResponsesRequest{
				Model:     tt.requestModel,
				Reasoning: tt.existingReason,
			}

			converted, err := adaptor.ConvertOpenAIResponsesRequest(nil, info, request)

			require.NoError(t, err)
			responsesRequest, ok := converted.(dto.OpenAIResponsesRequest)
			require.True(t, ok)
			assert.Equal(t, tt.wantModel, responsesRequest.Model)
			require.NotNil(t, responsesRequest.Reasoning)
			assert.Equal(t, tt.wantEffort, responsesRequest.Reasoning.Effort)
			assert.Equal(t, tt.wantSummary, responsesRequest.Reasoning.Summary)
			assert.Equal(t, tt.wantModel, info.UpstreamModelName)
			assert.Equal(t, tt.wantEffort, info.ReasoningEffort)
		})
	}
}
