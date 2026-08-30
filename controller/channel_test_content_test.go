package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveChannelTestEndpoint(t *testing.T) {
	testCases := []struct {
		name      string
		channel   *model.Channel
		modelName string
		requested string
		expected  constant.EndpointType
	}{
		{name: "explicit endpoint", modelName: "text-embedding-3-small", requested: "openai", expected: constant.EndpointTypeOpenAI},
		{name: "rerank", modelName: "jina-reranker-v2", expected: constant.EndpointTypeJinaRerank},
		{name: "embedding", modelName: "text-embedding-3-small", expected: constant.EndpointTypeEmbeddings},
		{name: "volc seedream", channel: &model.Channel{Type: constant.ChannelTypeVolcEngine}, modelName: "doubao-seedream-4", expected: constant.EndpointTypeImageGeneration},
		{name: "codex channel", channel: &model.Channel{Type: constant.ChannelTypeCodex}, modelName: "gpt-5", expected: constant.EndpointTypeOpenAIResponse},
		{name: "explicit compact endpoint", channel: &model.Channel{Type: constant.ChannelTypeCodex}, modelName: "gpt-5", requested: string(constant.EndpointTypeOpenAIResponseCompact), expected: constant.EndpointTypeOpenAIResponseCompact},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			endpoint, err := resolveChannelTestEndpoint(testCase.channel, testCase.modelName, testCase.requested)
			require.NoError(t, err)
			assert.Equal(t, testCase.expected, endpoint)
		})
	}
}

func TestBuildChannelTestRequestResolvesContentPriority(t *testing.T) {
	channelContent := "channel default"
	configBytes, err := common.Marshal(hostdto.ChannelTestRequestConfig{
		Version: hostdto.ChannelTestRequestConfigVersion,
		Overrides: map[string]hostdto.ChannelTestContentOverride{
			string(constant.EndpointTypeOpenAI): {Content: &channelContent},
		},
	})
	require.NoError(t, err)
	channel := &model.Channel{TestRequestConfig: string(configBytes)}

	request, err := buildChannelTestRequest("gpt-4o-mini", constant.EndpointTypeOpenAI, channel, false, nil)
	require.NoError(t, err)
	openAIRequest, ok := request.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	require.Equal(t, channelContent, openAIRequest.Messages[0].Content)

	request, err = buildChannelTestRequest("gpt-4o-mini", constant.EndpointTypeOpenAI, channel, false, map[string]hostdto.ChannelTestContentOverride{
		string(constant.EndpointTypeOpenAI): {Mode: "builtin"},
	})
	require.NoError(t, err)
	openAIRequest, ok = request.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, "hi", openAIRequest.Messages[0].Content)

	sessionContent := "session override"
	request, err = buildChannelTestRequest("gpt-4o-mini", constant.EndpointTypeOpenAI, channel, false, map[string]hostdto.ChannelTestContentOverride{
		string(constant.EndpointTypeOpenAI): {Mode: "custom", Content: &sessionContent},
	})
	require.NoError(t, err)
	openAIRequest, ok = request.(*dto.GeneralOpenAIRequest)
	require.True(t, ok)
	assert.Equal(t, sessionContent, openAIRequest.Messages[0].Content)
}

func TestBuildChannelTestRequestEncodesResponsesContent(t *testing.T) {
	content := "quoted \"value\"\n你好"
	request, err := buildChannelTestRequest("gpt-4o-mini", constant.EndpointTypeOpenAIResponse, nil, false, map[string]hostdto.ChannelTestContentOverride{
		string(constant.EndpointTypeOpenAIResponse): {Mode: "custom", Content: &content},
	})
	require.NoError(t, err)
	responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var input []map[string]string
	require.NoError(t, common.Unmarshal(responsesRequest.Input, &input))
	require.Len(t, input, 1)
	assert.Equal(t, content, input[0]["content"])
}

func TestGetChannelTestRequestConfigRejectsUnknownFields(t *testing.T) {
	channel := &model.Channel{TestRequestConfig: `{"version":1,"overrides":{"openai":{"content":"hello","unexpected":true}}}`}

	_, err := getChannelTestRequestConfig(channel)
	require.ErrorContains(t, err, "unknown field")
}
