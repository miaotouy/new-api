package relay

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/decisions"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func DecisionsHelper(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	info.InitChannelMeta(c)
	if !constant.SupportsDecisions(info.ChannelType, c.GetString("task_plugin_key")) {
		return types.NewError(errors.New(i18n.T(c, i18n.MsgRelayDecisionsUnsupportedChannel)), types.ErrorCodeInvalidRequest)
	}
	original, ok := info.Request.(*dto.DecisionsRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid decisions request: %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	request := *original
	if err := helper.ModelMappedHelper(c, info, &request); err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	adaptor := &decisions.Adaptor{}
	adaptor.Init(info)
	data, err := adaptor.ConvertDecisionsRequest(info, &request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if len(info.ParamOverride) > 0 {
		data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
	}
	// Overrides and passthrough extensions must not enable unsupported streaming
	// or bypass the same token-quantity bounds enforced on incoming requests.
	var outbound dto.DecisionsRequest
	if err := common.Unmarshal(data, &outbound); err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if err := outbound.Validate(); err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	if helper.ExceedsMaxTokensLimit(outbound.MaxTokens, outbound.MaxCompletionTokens, outbound.MaxOutputTokens) {
		return types.NewError(errors.New("max_tokens is invalid"), types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	body, closer, err := relaycommon.NewOutboundJSONBody(data)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	defer closer.Close()
	result, err := adaptor.DoRequest(c, info, body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusBadGateway)
	}
	response, ok := result.(*http.Response)
	if !ok || response == nil {
		return types.NewOpenAIError(errors.New("invalid decisions HTTP response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		apiErr := service.RelayErrorHandler(c.Request.Context(), response, false)
		service.ResetStatusCode(apiErr, c.GetString("status_code_mapping"))
		return apiErr
	}
	info.SetFirstResponseTime()
	// Decision answers are small JSON documents; bound memory even when the
	// upstream ignores the request protocol or emits an endless stream.
	const maxDecisionResponseBytes = 16 << 20
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxDecisionResponseBytes+1))
	if err != nil || len(raw) > maxDecisionResponseBytes {
		if err == nil {
			err = errors.New("decisions response is too large")
		}
		return types.NewOpenAIError(errors.New(i18n.T(c, i18n.MsgRelayDecisionsInvalidResponse, map[string]any{"Error": err.Error()})), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	usage, estimated, err := decisionResponseUsage(raw, &outbound, info.GetEstimatePromptTokens())
	if err != nil {
		return types.NewOpenAIError(errors.New(i18n.T(c, i18n.MsgRelayDecisionsInvalidResponse, map[string]any{"Error": err.Error()})), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	if estimated {
		logger.LogWarn(c, common.LogText("decisions response has invalid or missing token usage; retaining the input estimate"))
		common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	}
	if info.BillingRequestInput != nil {
		info.BillingRequestInput.Usage = map[string]any{"input_tokens": usage.PromptTokens, "output_tokens": usage.CompletionTokens}
	}
	if snap := info.TieredBillingSnapshot; snap != nil && snap.TaskUsageBilling && info.BillingRequestInput != nil {
		snap.UsageFacts = info.BillingRequestInput.Usage
	}
	// Settle before writing so a disconnected client cannot turn completed
	// upstream inference into a refunded request.
	service.PostTextConsumeQuota(c, info, usage, nil)
	c.Data(response.StatusCode, "application/json", raw)
	return nil
}

// decisionResponseUsage validates the answer contract without rebuilding the
// provider response. Missing/invalid usage retains the reservation estimate;
// explicit zero usage remains authoritative and can refund the reservation.
func decisionResponseUsage(raw []byte, request *dto.DecisionsRequest, estimatedTokens int) (*dto.Usage, bool, error) {
	var envelope struct {
		Model   string `json:"model"`
		Answers map[string]struct {
			Type string `json:"type"`
		} `json:"answers"`
		Usage json.RawMessage `json:"usage"`
	}
	if err := common.Unmarshal(raw, &envelope); err != nil {
		return nil, false, err
	}
	if envelope.Model == "" || envelope.Answers == nil {
		return nil, false, errors.New("decisions response is missing model or answers")
	}
	for id, question := range request.Questions {
		answer, exists := envelope.Answers[id]
		if !exists || answer.Type != question.Type {
			return nil, false, fmt.Errorf("decisions response is missing a matching answer for %q", id)
		}
	}
	var reported struct {
		InputTokens  *int64 `json:"input_tokens"`
		OutputTokens *int64 `json:"output_tokens"`
	}
	usageErr := common.Unmarshal(envelope.Usage, &reported)
	var outputTokens int64
	if reported.OutputTokens != nil {
		outputTokens = *reported.OutputTokens
	}
	if usageErr != nil || reported.InputTokens == nil || *reported.InputTokens < 0 || outputTokens < 0 || *reported.InputTokens > math.MaxInt32 || outputTokens > math.MaxInt32 {
		tokens := max(0, min(estimatedTokens, math.MaxInt32))
		return &dto.Usage{PromptTokens: tokens, TotalTokens: tokens}, true, nil
	}

	input, output := int(*reported.InputTokens), int(outputTokens)
	return &dto.Usage{PromptTokens: input, CompletionTokens: output, TotalTokens: input + output}, false, nil
}
