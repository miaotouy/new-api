package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	hostdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hosttypes "github.com/QuantumNous/new-api/types"

	"github.com/samber/lo"
	"github.com/tidwall/gjson"

	"github.com/gin-gonic/gin"
)

type testResult struct {
	context      *gin.Context
	localErr     error
	newAPIError  *types.NewAPIError
	endpointType string
}

func resolveChannelTestEndpoint(channel *model.Channel, modelName string, requestedEndpoint string) (constant.EndpointType, error) {
	requestedEndpoint = strings.TrimSpace(requestedEndpoint)
	if requestedEndpoint != "" && requestedEndpoint != "auto" {
		endpoint := constant.EndpointType(requestedEndpoint)
		if !hostdto.IsSupportedChannelTestEndpoint(endpoint) {
			return "", fmt.Errorf("unsupported channel test endpoint: %s", requestedEndpoint)
		}
		return endpoint, nil
	}

	modelNameLower := strings.ToLower(modelName)
	if strings.HasSuffix(modelName, ratio_setting.CompactModelSuffix) {
		return constant.EndpointTypeOpenAIResponseCompact, nil
	}
	if channel != nil && channel.Type == constant.ChannelTypeCodex {
		return constant.EndpointTypeOpenAIResponse, nil
	}
	if strings.Contains(modelNameLower, "rerank") {
		return constant.EndpointTypeJinaRerank, nil
	}
	if strings.Contains(modelNameLower, "embedding") || strings.HasPrefix(modelName, "m3e") || strings.Contains(modelName, "bge-") || strings.Contains(modelName, "embed") || (channel != nil && channel.Type == constant.ChannelTypeMokaAI) {
		return constant.EndpointTypeEmbeddings, nil
	}
	if channel != nil && channel.Type == constant.ChannelTypeVolcEngine && strings.Contains(modelNameLower, "seedream") {
		return constant.EndpointTypeImageGeneration, nil
	}
	if strings.Contains(modelNameLower, "codex") {
		return constant.EndpointTypeOpenAIResponse, nil
	}
	return constant.EndpointTypeOpenAI, nil
}

func relayFormatForChannelTestEndpoint(endpoint constant.EndpointType) types.RelayFormat {
	switch endpoint {
	case constant.EndpointTypeOpenAIResponse:
		return types.RelayFormatOpenAIResponses
	case constant.EndpointTypeOpenAIResponseCompact:
		return types.RelayFormatOpenAIResponsesCompaction
	case constant.EndpointTypeAnthropic:
		return types.RelayFormatClaude
	case constant.EndpointTypeGemini:
		return types.RelayFormatGemini
	case constant.EndpointTypeJinaRerank:
		return types.RelayFormatRerank
	case constant.EndpointTypeImageGeneration:
		return types.RelayFormatOpenAIImage
	case constant.EndpointTypeEmbeddings:
		return types.RelayFormatEmbedding
	default:
		return types.RelayFormatOpenAI
	}
}

func resolveChannelTestUserID(c *gin.Context) (int, error) {
	if c != nil {
		if userID := c.GetInt("id"); userID > 0 {
			return userID, nil
		}
	}

	var rootUser model.User
	if err := model.DB.Select("id").Where("role = ?", common.RoleRootUser).First(&rootUser).Error; err != nil {
		return 0, fmt.Errorf("failed to resolve channel test user: %w", err)
	}
	if rootUser.Id == 0 {
		return 0, errors.New("failed to resolve channel test user")
	}
	return rootUser.Id, nil
}

func testChannel(ctx context.Context, channel *model.Channel, testUserID int, testModel string, endpointType string, isStream bool, sessionOverrides ...map[string]hostdto.ChannelTestContentOverride) (result testResult) {
	resolvedEndpointName := ""
	defer func() {
		if result.endpointType == "" {
			result.endpointType = resolvedEndpointName
		}
	}()
	if ctx == nil {
		ctx = context.Background()
	}
	tik := time.Now()
	var unsupportedTestChannelTypes = []int{
		constant.ChannelTypeMidjourney,
		constant.ChannelTypeMidjourneyPlus,
		constant.ChannelTypeSunoAPI,
		constant.ChannelTypeKling,
		constant.ChannelTypeJimeng,
		constant.ChannelTypeDoubaoVideo,
		constant.ChannelTypeVidu,
	}
	if lo.Contains(unsupportedTestChannelTypes, channel.Type) {
		channelTypeName := constant.GetChannelTypeName(channel.Type)
		return testResult{
			localErr: fmt.Errorf("%s channel test is not supported", channelTypeName),
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	testModel = strings.TrimSpace(testModel)
	if testModel == "" {
		if channel.TestModel != nil && *channel.TestModel != "" {
			testModel = strings.TrimSpace(*channel.TestModel)
		} else {
			models := channel.GetModels()
			if len(models) > 0 {
				testModel = strings.TrimSpace(models[0])
			}
			if testModel == "" {
				testModel = "gpt-4o-mini"
			}
		}
	}

	resolvedEndpoint, err := resolveChannelTestEndpoint(channel, testModel, endpointType)
	if err != nil {
		return testResult{localErr: err}
	}
	endpointType = string(resolvedEndpoint)
	resolvedEndpointName = endpointType
	endpointInfo, _ := common.GetDefaultEndpointInfo(resolvedEndpoint)
	requestPath := endpointInfo.Path
	if strings.HasPrefix(requestPath, "/v1/responses/compact") {
		testModel = ratio_setting.WithCompactModelSuffix(testModel)
	}

	c.Request = httptest.NewRequestWithContext(ctx, http.MethodPost, requestPath, nil)

	cache, err := model.GetUserCache(testUserID)
	if err != nil {
		return testResult{
			localErr:    err,
			newAPIError: nil,
		}
	}
	cache.WriteContext(c)
	c.Set("id", testUserID)

	//c.Request.Header.Set("Authorization", "Bearer "+channel.Key)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("channel", channel.Type)
	c.Set("base_url", channel.GetBaseURL())
	group, _ := model.GetUserGroup(testUserID, false)
	c.Set("group", group)

	newAPIError := middleware.SetupContextForSelectedChannel(c, channel, testModel)
	if newAPIError != nil {
		return testResult{
			context:     c,
			localErr:    newAPIError,
			newAPIError: newAPIError,
		}
	}

	relayFormat := relayFormatForChannelTestEndpoint(resolvedEndpoint)

	var overrides map[string]hostdto.ChannelTestContentOverride
	if len(sessionOverrides) > 0 {
		overrides = sessionOverrides[0]
	}
	request, err := buildChannelTestRequest(testModel, resolvedEndpoint, channel, isStream, overrides)
	if err != nil {
		return testResult{localErr: err}
	}

	info, err := relaycommon.GenRelayInfo(c, relayFormat, request, nil)

	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeGenRelayInfoFailed),
		}
	}

	info.IsChannelTest = true
	info.InitChannelMeta(c)

	err = attachTestBillingRequestInput(info, request)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeChannelModelMappedError),
		}
	}

	testModel = info.UpstreamModelName
	// 更新请求中的模型名称
	request.SetModelName(testModel)

	apiType, _ := common.ChannelType2APIType(channel.Type)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!common.IsResponsesCompactAPIType(apiType) {
		return testResult{
			context:     c,
			localErr:    fmt.Errorf("responses compaction test is not supported for api type %d", apiType),
			newAPIError: types.NewError(fmt.Errorf("unsupported api type: %d", apiType), types.ErrorCodeInvalidApiType),
		}
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		return testResult{
			context:     c,
			localErr:    fmt.Errorf("invalid api type: %d, adaptor is nil", apiType),
			newAPIError: types.NewError(fmt.Errorf("invalid api type: %d, adaptor is nil", apiType), types.ErrorCodeInvalidApiType),
		}
	}

	//// 创建一个用于日志的 info 副本，移除 ApiKey
	//logInfo := info
	//logInfo.ApiKey = ""
	common.SysLog(fmt.Sprintf("testing channel %d with model %s , info %+v ", channel.Id, testModel, info.ToString()))

	priceData, err := helper.ModelPriceHelper(c, info, 0, request.GetTokenCountMeta())
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithStatusCode(http.StatusBadRequest)),
		}
	}

	adaptor.Init(info)

	var convertedRequest any
	// 根据 RelayMode 选择正确的转换函数
	switch info.RelayMode {
	case relayconstant.RelayModeEmbeddings:
		// Embedding 请求 - request 已经是正确的类型
		if embeddingReq, ok := request.(*dto.EmbeddingRequest); ok {
			convertedRequest, err = adaptor.ConvertEmbeddingRequest(c, info, *embeddingReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid embedding request type"),
				newAPIError: types.NewError(errors.New("invalid embedding request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeImagesGenerations:
		// 图像生成请求 - request 已经是正确的类型
		if imageReq, ok := request.(*dto.ImageRequest); ok {
			convertedRequest, err = adaptor.ConvertImageRequest(c, info, *imageReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid image request type"),
				newAPIError: types.NewError(errors.New("invalid image request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeRerank:
		// Rerank 请求 - request 已经是正确的类型
		if rerankReq, ok := request.(*dto.RerankRequest); ok {
			convertedRequest, err = adaptor.ConvertRerankRequest(c, info.RelayMode, *rerankReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid rerank request type"),
				newAPIError: types.NewError(errors.New("invalid rerank request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeResponses:
		// Response 请求 - request 已经是正确的类型
		if responseReq, ok := request.(*dto.OpenAIResponsesRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *responseReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid response request type"),
				newAPIError: types.NewError(errors.New("invalid response request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	case relayconstant.RelayModeResponsesCompact:
		// Response compaction request - convert to OpenAIResponsesRequest before adapting
		switch req := request.(type) {
		case *dto.OpenAIResponsesCompactionRequest:
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, dto.OpenAIResponsesRequest{
				Model:              req.Model,
				Input:              req.Input,
				Instructions:       req.Instructions,
				PreviousResponseID: req.PreviousResponseID,
			})
		case *dto.OpenAIResponsesRequest:
			convertedRequest, err = adaptor.ConvertOpenAIResponsesRequest(c, info, *req)
		default:
			return testResult{
				context:     c,
				localErr:    errors.New("invalid response compaction request type"),
				newAPIError: types.NewError(errors.New("invalid response compaction request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	default:
		// Chat/Completion 等其他请求类型
		if generalReq, ok := request.(*dto.GeneralOpenAIRequest); ok {
			convertedRequest, err = adaptor.ConvertOpenAIRequest(c, info, generalReq)
		} else {
			return testResult{
				context:     c,
				localErr:    errors.New("invalid general request type"),
				newAPIError: types.NewError(errors.New("invalid general request type"), types.ErrorCodeConvertRequestFailed),
			}
		}
	}

	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
		}
	}
	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewError(err, types.ErrorCodeJsonMarshalFailed),
		}
	}

	//jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings)
	//if err != nil {
	//	return testResult{
	//		context:     c,
	//		localErr:    err,
	//		newAPIError: types.NewError(err, types.ErrorCodeConvertRequestFailed),
	//	}
	//}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			if fixedErr, ok := relaycommon.AsParamOverrideReturnError(err); ok {
				return testResult{
					context:     c,
					localErr:    fixedErr,
					newAPIError: relaycommon.NewAPIErrorFromParamOverride(fixedErr),
				}
			}
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid),
			}
		}
	}

	requestBody := bytes.NewBuffer(jsonData)
	c.Request.Body = io.NopCloser(bytes.NewBuffer(jsonData))
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError),
		}
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		if httpResp.StatusCode != http.StatusOK {
			err := service.RelayErrorHandler(c.Request.Context(), httpResp, true)
			common.SysError(fmt.Sprintf(
				"channel test bad response: channel_id=%d name=%s type=%d model=%s endpoint_type=%s status=%d err=%v",
				channel.Id,
				channel.Name,
				channel.Type,
				testModel,
				endpointType,
				httpResp.StatusCode,
				err,
			))
			return testResult{
				context:     c,
				localErr:    err,
				newAPIError: types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError),
			}
		}
	}
	usageA, respErr := adaptor.DoResponse(c, httpResp, info)
	if respErr != nil {
		return testResult{
			context:     c,
			localErr:    respErr,
			newAPIError: respErr,
		}
	}
	usage, usageErr := coerceTestUsage(usageA, isStream, info.GetEstimatePromptTokens())
	if usageErr != nil {
		return testResult{
			context:     c,
			localErr:    usageErr,
			newAPIError: types.NewOpenAIError(usageErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}
	responseResult := w.Result()
	respBody, err := readTestResponseBody(responseResult.Body, isStream)
	if err != nil {
		return testResult{
			context:     c,
			localErr:    err,
			newAPIError: types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError),
		}
	}
	if bodyErr := validateTestResponseBody(respBody, isStream); bodyErr != nil {
		return testResult{
			context:     c,
			localErr:    bodyErr,
			newAPIError: types.NewOpenAIError(bodyErr, types.ErrorCodeBadResponseBody, http.StatusInternalServerError),
		}
	}
	info.SetEstimatePromptTokens(usage.PromptTokens)

	quota, tieredResult := settleTestQuota(info, priceData, usage)
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	consumedTime := float64(milliseconds) / 1000.0
	other := buildTestLogOther(c, info, priceData, usage, tieredResult)
	model.RecordConsumeLog(c, testUserID, model.RecordConsumeLogParams{
		ChannelId:        channel.Id,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
		ModelName:        info.OriginModelName,
		TokenName:        "模型测试",
		Quota:            quota,
		Content:          "模型测试",
		UseTimeSeconds:   int(consumedTime),
		IsStream:         info.IsStream,
		Group:            info.UsingGroup,
		Other:            other,
	})
	common.SysLog(fmt.Sprintf("testing channel #%d, response: \n%s", channel.Id, string(respBody)))
	return testResult{
		context:      c,
		localErr:     nil,
		newAPIError:  nil,
		endpointType: endpointType,
	}
}

func attachTestBillingRequestInput(info *relaycommon.RelayInfo, request dto.Request) error {
	if info == nil {
		return nil
	}

	input, err := helper.BuildBillingExprRequestInputFromRequest(request, info.RequestHeaders)
	if err != nil {
		return err
	}
	info.BillingRequestInput = &input
	return nil
}

func settleTestQuota(info *relaycommon.RelayInfo, priceData hosttypes.PriceData, usage *dto.Usage) (int, *billingexpr.TieredResult) {
	if usage != nil && info != nil && info.TieredBillingSnapshot != nil {
		isClaudeUsageSemantic := usage.UsageSemantic == "anthropic" || info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
		usedVars := billingexpr.UsedVars(info.TieredBillingSnapshot.ExprString)
		if ok, quota, result := service.TryTieredSettle(info, service.BuildTieredTokenParams(usage, isClaudeUsageSemantic, usedVars)); ok {
			return quota, result
		}
	}

	quota := 0
	if !priceData.UsePrice {
		quota = usage.PromptTokens + int(math.Round(float64(usage.CompletionTokens)*priceData.CompletionRatio))
		quota = int(math.Round(float64(quota) * priceData.ModelRatio))
		if priceData.ModelRatio != 0 && quota <= 0 {
			quota = 1
		}
		return quota, nil
	}

	return int(priceData.ModelPrice * common.QuotaPerUnit), nil
}

func buildTestLogOther(c *gin.Context, info *relaycommon.RelayInfo, priceData hosttypes.PriceData, usage *dto.Usage, tieredResult *billingexpr.TieredResult) map[string]interface{} {
	other := service.GenerateTextOtherInfo(c, info, priceData.ModelRatio, priceData.GroupRatioInfo.GroupRatio, priceData.CompletionRatio,
		usage.PromptTokensDetails.CachedTokens, priceData.CacheRatio, priceData.ModelPrice, priceData.GroupRatioInfo.GroupSpecialRatio)
	if tieredResult != nil {
		service.InjectTieredBillingInfo(other, info, tieredResult)
	}
	return other
}

func coerceTestUsage(usageAny any, isStream bool, estimatePromptTokens int) (*dto.Usage, error) {
	switch u := usageAny.(type) {
	case *dto.Usage:
		return u, nil
	case dto.Usage:
		return &u, nil
	case nil:
		if !isStream {
			return nil, errors.New("usage is nil")
		}
		usage := &dto.Usage{
			PromptTokens: estimatePromptTokens,
		}
		usage.TotalTokens = usage.PromptTokens
		return usage, nil
	default:
		if !isStream {
			return nil, fmt.Errorf("invalid usage type: %T", usageAny)
		}
		usage := &dto.Usage{
			PromptTokens: estimatePromptTokens,
		}
		usage.TotalTokens = usage.PromptTokens
		return usage, nil
	}
}

func readTestResponseBody(body io.ReadCloser, isStream bool) ([]byte, error) {
	defer func() { _ = body.Close() }()
	const maxStreamLogBytes = 8 << 10
	if isStream {
		return io.ReadAll(io.LimitReader(body, maxStreamLogBytes))
	}
	return io.ReadAll(body)
}

func detectErrorFromTestResponseBody(respBody []byte) error {
	b := bytes.TrimSpace(respBody)
	if len(b) == 0 {
		return nil
	}
	if message := detectErrorMessageFromJSONBytes(b); message != "" {
		return fmt.Errorf("upstream error: %s", message)
	}

	for _, line := range bytes.Split(b, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		if message := detectErrorMessageFromJSONBytes(payload); message != "" {
			return fmt.Errorf("upstream error: %s", message)
		}
	}

	return nil
}

func validateStreamTestResponseBody(respBody []byte) error {
	b := bytes.TrimSpace(respBody)
	if len(b) == 0 {
		return errors.New("stream response body is empty")
	}

	for _, line := range bytes.Split(b, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		if len(payload) == 0 || bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}

		return nil
	}

	return errors.New("stream response body does not contain a valid stream event")
}

func validateTestResponseBody(respBody []byte, isStream bool) error {
	if bodyErr := detectErrorFromTestResponseBody(respBody); bodyErr != nil {
		return bodyErr
	}
	if isStream {
		return validateStreamTestResponseBody(respBody)
	}
	return nil
}

func shouldUseStreamForAutomaticChannelTest(channel *model.Channel) bool {
	return channel != nil && channel.Type == constant.ChannelTypeCodex
}

func detectErrorMessageFromJSONBytes(jsonBytes []byte) string {
	if len(jsonBytes) == 0 {
		return ""
	}
	if jsonBytes[0] != '{' && jsonBytes[0] != '[' {
		return ""
	}
	errVal := gjson.GetBytes(jsonBytes, "error")
	if !errVal.Exists() || errVal.Type == gjson.Null {
		return ""
	}

	message := gjson.GetBytes(jsonBytes, "error.message").String()
	if message == "" {
		message = gjson.GetBytes(jsonBytes, "error.error.message").String()
	}
	if message == "" && errVal.Type == gjson.String {
		message = errVal.String()
	}
	if message == "" {
		message = errVal.Raw
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return "upstream returned error payload"
	}
	return message
}

func builtinChannelTestOverrides() map[string]hostdto.ChannelTestContentOverride {
	openAIContent := "hi"
	anthropicContent := "hi"
	geminiContent := "hi"
	responsesContent := "hi"
	compactContent := "hi"
	input := "hello world"
	prompt := "a cute cat"
	query := "What is Deep Learning?"
	return map[string]hostdto.ChannelTestContentOverride{
		string(constant.EndpointTypeOpenAI):                {Content: &openAIContent},
		string(constant.EndpointTypeAnthropic):             {Content: &anthropicContent},
		string(constant.EndpointTypeGemini):                {Content: &geminiContent},
		string(constant.EndpointTypeOpenAIResponse):        {Content: &responsesContent},
		string(constant.EndpointTypeOpenAIResponseCompact): {Content: &compactContent},
		string(constant.EndpointTypeEmbeddings):            {Input: &input},
		string(constant.EndpointTypeImageGeneration):       {Prompt: &prompt},
		string(constant.EndpointTypeJinaRerank): {
			Query:     &query,
			Documents: []string{"Deep Learning is a subset of machine learning.", "Machine learning is a field of artificial intelligence."},
		},
	}
}

func getChannelTestRequestConfig(channel *model.Channel) (hostdto.ChannelTestRequestConfig, error) {
	if channel == nil || strings.TrimSpace(channel.TestRequestConfig) == "" {
		return hostdto.ChannelTestRequestConfig{Version: hostdto.ChannelTestRequestConfigVersion, Overrides: map[string]hostdto.ChannelTestContentOverride{}}, nil
	}
	config := hostdto.ChannelTestRequestConfig{}
	if err := common.UnmarshalJsonStrStrict(channel.TestRequestConfig, &config); err != nil {
		return hostdto.ChannelTestRequestConfig{}, fmt.Errorf("invalid channel test request config: %w", err)
	}
	if err := hostdto.ValidateChannelTestRequestConfig(config, false); err != nil {
		return hostdto.ChannelTestRequestConfig{}, fmt.Errorf("invalid channel test request config: %w", err)
	}
	return config, nil
}

func resolveChannelTestContent(channel *model.Channel, endpoint constant.EndpointType, sessionOverrides map[string]hostdto.ChannelTestContentOverride) (hostdto.ChannelTestContentOverride, error) {
	if sessionOverride, ok := sessionOverrides[string(endpoint)]; ok {
		if err := hostdto.ValidateChannelTestContentOverride(endpoint, sessionOverride, true); err != nil {
			return hostdto.ChannelTestContentOverride{}, err
		}
		if sessionOverride.Mode == "custom" {
			sessionOverride.Mode = ""
			return sessionOverride, nil
		}
		return builtinChannelTestOverrides()[string(endpoint)], nil
	}
	config, err := getChannelTestRequestConfig(channel)
	if err != nil {
		return hostdto.ChannelTestContentOverride{}, err
	}
	if override, ok := config.Overrides[string(endpoint)]; ok {
		return override, nil
	}
	return builtinChannelTestOverrides()[string(endpoint)], nil
}

func buildChannelTestRequest(modelName string, endpoint constant.EndpointType, channel *model.Channel, isStream bool, sessionOverrides map[string]hostdto.ChannelTestContentOverride) (dto.Request, error) {
	override, err := resolveChannelTestContent(channel, endpoint, sessionOverrides)
	if err != nil {
		return nil, err
	}
	switch endpoint {
	case constant.EndpointTypeEmbeddings:
		return &dto.EmbeddingRequest{Model: modelName, Input: []any{*override.Input}}, nil
	case constant.EndpointTypeImageGeneration:
		return &dto.ImageRequest{Model: modelName, Prompt: *override.Prompt, N: lo.ToPtr(uint(1)), Size: "1024x1024"}, nil
	case constant.EndpointTypeJinaRerank:
		documents := make([]any, len(override.Documents))
		for index, document := range override.Documents {
			documents[index] = document
		}
		return &dto.RerankRequest{Model: modelName, Query: *override.Query, Documents: documents, TopN: lo.ToPtr(2)}, nil
	case constant.EndpointTypeOpenAIResponse:
		responsesInput, err := common.Marshal([]map[string]string{{"role": "user", "content": *override.Content}})
		if err != nil {
			return nil, err
		}
		return &dto.OpenAIResponsesRequest{Model: modelName, Input: json.RawMessage(responsesInput), Stream: lo.ToPtr(isStream)}, nil
	case constant.EndpointTypeOpenAIResponseCompact:
		responsesInput, err := common.Marshal([]map[string]string{{"role": "user", "content": *override.Content}})
		if err != nil {
			return nil, err
		}
		return &dto.OpenAIResponsesCompactionRequest{Model: modelName, Input: json.RawMessage(responsesInput)}, nil
	case constant.EndpointTypeAnthropic, constant.EndpointTypeGemini, constant.EndpointTypeOpenAI:
		maxTokens := uint(16)
		if endpoint == constant.EndpointTypeGemini {
			maxTokens = 3000
		}
		req := &dto.GeneralOpenAIRequest{Model: modelName, Stream: lo.ToPtr(isStream), Messages: []dto.Message{{Role: "user", Content: *override.Content}}, MaxTokens: lo.ToPtr(maxTokens)}
		if isStream {
			req.StreamOptions = &dto.StreamOptions{IncludeUsage: true}
		}
		return req, nil
	default:
		return nil, fmt.Errorf("unsupported channel test endpoint: %s", endpoint)
	}
}

func buildTestRequest(modelName string, endpointType string, channel *model.Channel, isStream bool) dto.Request {
	endpoint, err := resolveChannelTestEndpoint(channel, modelName, endpointType)
	if err != nil {
		return nil
	}
	request, err := buildChannelTestRequest(modelName, endpoint, channel, isStream, nil)
	if err != nil {
		return nil
	}
	return request
}

func TestChannel(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channel, err := model.CacheGetChannel(channelId)
	if err != nil {
		channel, err = model.GetChannelById(channelId, true)
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	//defer func() {
	//	if channel.ChannelInfo.IsMultiKey {
	//		go func() { _ = channel.SaveChannelInfo() }()
	//	}
	//}()
	testModel := c.Query("model")
	endpointType := c.Query("endpoint_type")
	isStream, _ := strconv.ParseBool(c.Query("stream"))
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tik := time.Now()
	requestCtx := context.Background()
	if c.Request != nil {
		requestCtx = c.Request.Context()
	}
	result := testChannel(requestCtx, channel, testUserID, testModel, endpointType, isStream)
	if result.localErr != nil {
		resp := gin.H{
			"success": false,
			"message": result.localErr.Error(),
			"time":    0.0,
		}
		if result.newAPIError != nil {
			resp["error_code"] = result.newAPIError.GetErrorCode()
		}
		if result.endpointType != "" {
			resp["endpoint_type"] = result.endpointType
		}
		c.JSON(http.StatusOK, resp)
		return
	}
	tok := time.Now()
	milliseconds := tok.Sub(tik).Milliseconds()
	go channel.UpdateResponseTime(milliseconds)
	consumedTime := float64(milliseconds) / 1000.0
	if result.newAPIError != nil {
		response := gin.H{
			"success":    false,
			"message":    result.newAPIError.Error(),
			"time":       consumedTime,
			"error_code": result.newAPIError.GetErrorCode(),
		}
		if result.endpointType != "" {
			response["endpoint_type"] = result.endpointType
		}
		c.JSON(http.StatusOK, response)
		return
	}
	response := gin.H{
		"success": true,
		"message": "",
		"time":    consumedTime,
	}
	if result.endpointType != "" {
		response["endpoint_type"] = result.endpointType
	}
	c.JSON(http.StatusOK, response)
}

func getChannelForTest(c *gin.Context) (*model.Channel, int, bool) {
	channelID, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, err)
		return nil, 0, false
	}
	channel, err := model.CacheGetChannel(channelID)
	if err != nil {
		channel, err = model.GetChannelById(channelID, true)
		if err != nil {
			common.ApiError(c, err)
			return nil, 0, false
		}
	}
	return channel, channelID, true
}

func writeChannelTestResult(c *gin.Context, result testResult, consumedTime float64) {
	response := gin.H{"success": result.localErr == nil && result.newAPIError == nil, "message": "", "time": consumedTime}
	if result.endpointType != "" {
		response["endpoint_type"] = result.endpointType
	}
	if result.localErr != nil {
		response["message"] = result.localErr.Error()
		if result.newAPIError != nil {
			response["error_code"] = result.newAPIError.GetErrorCode()
		}
		c.JSON(http.StatusOK, response)
		return
	}
	if result.newAPIError != nil {
		response["message"] = result.newAPIError.Error()
		response["error_code"] = result.newAPIError.GetErrorCode()
	}
	c.JSON(http.StatusOK, response)
}

func TestChannelWithRequest(c *gin.Context) {
	channel, _, ok := getChannelForTest(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	request := hostdto.ChannelTestRequest{}
	if err := common.DecodeJsonStrict(c.Request.Body, &request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel test request"})
		return
	}
	if err := hostdto.ValidateChannelTestOverrides(request.TestRequestOverrides); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	testUserID, err := resolveChannelTestUserID(c)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	tik := time.Now()
	result := testChannel(c.Request.Context(), channel, testUserID, request.Model, request.EndpointType, request.Stream, request.TestRequestOverrides)
	milliseconds := time.Since(tik).Milliseconds()
	if result.localErr == nil && result.newAPIError == nil {
		go channel.UpdateResponseTime(milliseconds)
	}
	writeChannelTestResult(c, result, float64(milliseconds)/1000)
}

func GetChannelTestConfig(c *gin.Context) {
	channel, _, ok := getChannelForTest(c)
	if !ok {
		return
	}
	config, err := getChannelTestRequestConfig(channel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": hostdto.ChannelTestConfigPanelData{
			Version:          hostdto.ChannelTestRequestConfigVersion,
			BuiltinOverrides: builtinChannelTestOverrides(),
			Overrides:        config.Overrides,
		},
	})
}

func UpdateChannelTestConfig(c *gin.Context) {
	_, channelID, ok := getChannelForTest(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	config := hostdto.ChannelTestRequestConfig{}
	if err := common.DecodeJsonStrict(c.Request.Body, &config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid channel test request config"})
		return
	}
	if err := hostdto.ValidateChannelTestRequestConfig(config, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	serialized, err := common.Marshal(config)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channelID).Update("test_request_config", string(serialized)).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	model.InitChannelCache()
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}

// channelTestSummary records the outcome of one channel test cycle so the
// system task can persist a per-run result for history.
type channelTestSummary struct {
	Tested    int `json:"tested"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Disabled  int `json:"disabled"`
	Enabled   int `json:"enabled"`
}

// performChannelTests runs the channel test loop synchronously, honoring ctx
// cancellation so a system-task runner that loses its lease stops promptly. When
// report is non-nil it is called after each channel with (processed, total) so
// the system task can surface progress.
func performChannelTests(ctx context.Context, channels []*model.Channel, testUserID int, allowDisable bool, report func(processed, total int)) channelTestSummary {
	summary := channelTestSummary{}
	var disableThreshold = int64(common.ChannelDisableThreshold * 1000)
	if disableThreshold == 0 {
		disableThreshold = 10000000 // a impossible value
	}

	total := len(channels)
	for index, channel := range channels {
		if ctx != nil && ctx.Err() != nil {
			break
		}
		if report != nil {
			report(index, total) // channels completed before this one
		}
		if channel.Status == common.ChannelStatusManuallyDisabled {
			continue
		}
		isChannelEnabled := channel.Status == common.ChannelStatusEnabled
		tik := time.Now()
		result := testChannel(ctx, channel, testUserID, "", "", shouldUseStreamForAutomaticChannelTest(channel))
		tok := time.Now()
		milliseconds := tok.Sub(tik).Milliseconds()
		if ctx != nil && ctx.Err() != nil {
			break
		}

		summary.Tested++

		shouldBanChannel := false
		newAPIError := result.newAPIError
		// request error disables the channel
		if newAPIError != nil {
			shouldBanChannel = service.ShouldDisableChannel(result.newAPIError)
		}

		// 当错误检查通过，才检查响应时间
		if common.AutomaticDisableChannelEnabled && !shouldBanChannel {
			if milliseconds > disableThreshold {
				err := fmt.Errorf("响应时间 %.2fs 超过阈值 %.2fs", float64(milliseconds)/1000.0, float64(disableThreshold)/1000.0)
				newAPIError = types.NewOpenAIError(err, types.ErrorCodeChannelResponseTimeExceeded, http.StatusRequestTimeout)
				shouldBanChannel = true
			}
		}

		if result.localErr == nil && newAPIError == nil {
			summary.Succeeded++
		} else {
			summary.Failed++
		}

		// disable channel
		if allowDisable && isChannelEnabled && shouldBanChannel && channel.GetAutoBan() {
			processChannelError(result.context, *types.NewChannelError(channel.Id, channel.Type, channel.Name, channel.ChannelInfo.IsMultiKey, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.GetAutoBan()), newAPIError)
			summary.Disabled++
		}

		// enable channel
		if result.localErr == nil && !isChannelEnabled && service.ShouldEnableChannel(newAPIError, channel.Status) {
			service.EnableChannel(channel.Id, common.GetContextKeyString(result.context, constant.ContextKeyChannelKey), channel.Name)
			summary.Enabled++
		}

		channel.UpdateResponseTime(milliseconds)
		if common.RequestInterval > 0 {
			if ctx == nil {
				time.Sleep(common.RequestInterval)
			} else {
				select {
				case <-ctx.Done():
					return summary
				case <-time.After(common.RequestInterval):
				}
			}
		}
	}
	if report != nil && (ctx == nil || ctx.Err() == nil) {
		report(total, total) // mark complete only when the full set was tested
	}
	return summary
}

// runChannelTestTask runs one synchronous channel test cycle for the system task
// runner (both the scheduled job and the manual "test all channels" trigger go
// through here). It honors ctx cancellation so a runner that loses its lease
// stops promptly. mode selects the channel set: an empty mode falls back to the
// configured monitor ChannelTestMode (scheduled behavior), while a manual
// trigger passes ChannelTestModeScheduledAll to test every channel. When notify
// is set the root user is notified on completion. Cross-instance execution is
// guarded by the system task per-type lock, so no process-local guard is needed.
func runChannelTestTask(ctx context.Context, mode string, notify bool, report func(processed, total int)) (channelTestSummary, error) {
	testUserID, err := resolveChannelTestUserID(nil)
	if err != nil {
		return channelTestSummary{}, err
	}
	channels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		return channelTestSummary{}, err
	}
	if strings.TrimSpace(mode) == "" {
		mode = operation_setting.GetMonitorSetting().ChannelTestMode
	}
	selected := selectChannelsForAutomaticTest(channels, mode)
	allowDisable := mode != operation_setting.ChannelTestModePassiveRecovery
	summary := performChannelTests(ctx, selected, testUserID, allowDisable, report)
	if notify && (ctx == nil || ctx.Err() == nil) {
		service.NotifyRootUser(dto.NotifyTypeChannelTest, "通道测试完成", "所有通道测试已完成")
	}
	return summary, nil
}

func selectChannelsForAutomaticTest(channels []*model.Channel, mode string) []*model.Channel {
	selected := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if channel.Status == common.ChannelStatusManuallyDisabled {
			continue
		}
		if mode == operation_setting.ChannelTestModePassiveRecovery && channel.Status != common.ChannelStatusAutoDisabled {
			continue
		}
		selected = append(selected, channel)
	}
	return selected
}

// TestAllChannels enqueues a channel_test system task instead of running the
// test loop inline. If any channel_test task is already active, the manual run is
// rejected so the caller does not mistake a scheduled run for this manual one.
func TestAllChannels(c *gin.Context) {
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeChannelTest, channelTestTaskPayload{
		Mode:   operation_setting.ChannelTestModeScheduledAll,
		Notify: true,
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": "已有通道测试任务正在运行或等待中，不能启动本次手动任务",
			"data": gin.H{
				"task_id": task.TaskID,
				"status":  task.Status,
				"type":    task.Type,
			},
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
		},
	})
}
