package plugins

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadBuiltinPlugin(t *testing.T, key string) (*jsplugin.LoadedPlugin, *jsplugin.Registry) {
	t.Helper()
	source, err := Source(key)
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	plugin, err := registry.RegisterFactory(source, jsplugin.Options{Key: key})
	require.NoError(t, err)
	return plugin, registry
}

func callPluginHook(t *testing.T, plugin *jsplugin.LoadedPlugin, hook string, args ...any) map[string]any {
	t.Helper()
	value, err := plugin.Engine.Call(t.Context(), hook, args...)
	require.NoError(t, err, hook)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, common.Unmarshal(encoded, &decoded))
	return decoded
}

func typeSafeSystemOneRequest() map[string]any {
	return map[string]any{
		"model": "jev-latest",
		"state": map[string]any{"ticket": "charged twice", "attempts": 3},
		"questions": map[string]any{
			"category": map[string]any{
				"type":         "choice",
				"instructions": "What is this ticket about?",
				"criteria":     map[string]any{"billing": "Money questions", "technical": nil},
			},
		},
	}
}

func TestBuiltInTypeSafeSystemOneContract(t *testing.T) {
	plugin, registry := loadBuiltinPlugin(t, "typesafe")

	_, found := registry.Generation().LookupDeclaredRoute("POST", "/v1/systemone")
	assert.False(t, found, "the main relay owns synchronous decisions")

	require.Equal(t, []string{"jev-1.13.0", "jev-latest", "jev-preview"}, plugin.Meta.Models)
	assert.True(t, plugin.Meta.SupportsUpstream(jsplugin.UpstreamKindNewAPI))
	require.Contains(t, plugin.Meta.UsageSchema, "input_tokens")
	assert.NotEmpty(t, plugin.Meta.UsageSchema["input_tokens"].Description)
	assert.Equal(t, "token", plugin.Meta.UsageSchema["input_tokens"].Unit)
	for _, hook := range []string{"buildSubmitRequest", "parseSubmitResponse", "extractUsage", "extractUsageOnComplete"} {
		callable, callableErr := plugin.Engine.HasCallablePath(t.Context(), hook)
		require.NoError(t, callableErr)
		assert.True(t, callable, hook)
	}
	for _, hook := range []string{"decodeSystemOne", "renderSystemOne", "error"} {
		callable, callableErr := plugin.Engine.HasCallablePath(t.Context(), "native", hook)
		require.NoError(t, callableErr)
		assert.True(t, callable, hook)
	}
}

func TestBuiltInTypeSafeBuildsSystemOneRequests(t *testing.T) {
	plugin, _ := loadBuiltinPlugin(t, "typesafe")
	request := typeSafeSystemOneRequest()

	for _, testCase := range []struct {
		name        string
		upstream    map[string]any
		baseURL     string
		upstreamID  string
		wantURL     string
		wantRequest map[string]any
	}{
		{
			name:       "vendor",
			upstream:   map[string]any{"kind": "vendor"},
			baseURL:    "https://api.typesafe.ai",
			upstreamID: "jev-1.13.0",
			wantURL:    "https://api.typesafe.ai/v1/systemone",
			wantRequest: map[string]any{
				"model": "jev-1.13.0", "state": request["state"], "questions": request["questions"],
			},
		},
		{
			name:       "new_api gateway uses the same route path",
			upstream:   map[string]any{"kind": "new_api"},
			baseURL:    "https://gateway.example/v1",
			upstreamID: "jev-latest",
			wantURL:    "https://gateway.example/v1/systemone",
			wantRequest: map[string]any{
				"model": "jev-latest", "state": request["state"], "questions": request["questions"],
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			descriptor := callPluginHook(t, plugin, "buildSubmitRequest", map[string]any{
				"baseUrl":       testCase.baseURL,
				"apiKey":        "ts-key",
				"model":         "jev-latest",
				"upstreamModel": testCase.upstreamID,
				"upstream":      testCase.upstream,
				"requestBody":   request,
			})
			assert.Equal(t, "POST", descriptor["method"])
			assert.Equal(t, testCase.wantURL, descriptor["url"])
			headers := descriptor["headers"].(map[string]any)
			assert.Equal(t, "Bearer ts-key", headers["Authorization"])
			body := descriptor["body"].(map[string]any)
			for key, want := range testCase.wantRequest {
				wantJSON, err := common.Marshal(want)
				require.NoError(t, err)
				gotJSON, err := common.Marshal(body[key])
				require.NoError(t, err)
				assert.JSONEq(t, string(wantJSON), string(gotJSON), key)
			}
		})
	}
}

func TestBuiltInTypeSafeReservesContextAndSettlesMeasuredUsage(t *testing.T) {
	plugin, _ := loadBuiltinPlugin(t, "typesafe")
	request := typeSafeSystemOneRequest()

	estimate := callPluginHook(t, plugin, "extractUsage", map[string]any{
		"model": "jev-latest", "upstreamModel": "jev-latest", "usagePurpose": "facts", "requestBody": request,
	})
	assert.Equal(t, float64(65536), estimate["input_tokens"], "reserve the documented 64k context budget")

	ratios, err := plugin.Engine.Call(t.Context(), "extractUsage", map[string]any{
		"model": "jev-latest", "upstreamModel": "jev-latest", "usagePurpose": "billing_ratios", "requestBody": request,
	})
	require.NoError(t, err)
	assert.Nil(t, ratios, "token billing uses the frozen task expression, not request ratios")

	responseBody := map[string]any{
		"model":   "jev-1.13.0",
		"answers": map[string]any{"category": map[string]any{"type": "choice", "choice": "billing", "probabilities": map[string]any{"billing": 1.0}, "confidence": 0.9}},
		"usage":   map[string]any{"input_tokens": 296, "output_tokens": 20},
	}
	parsed := callPluginHook(t, plugin, "parseSubmitResponse", map[string]any{
		"model": "jev-latest", "publicTaskId": "task_pub", "requestBody": request,
	}, map[string]any{"statusCode": 200, "body": responseBody})
	assert.Equal(t, "task_pub", parsed["taskId"])
	assert.Equal(t, "SUCCESS", parsed["immediate"].(map[string]any)["status"])

	facts := callPluginHook(t, plugin, "extractUsageOnComplete", map[string]any{}, map[string]any{"status": "SUCCESS"}, responseBody)
	assert.Equal(t, float64(296), facts["input_tokens"])

	rendered, err := plugin.Engine.CallMember(t.Context(), "native", "renderSystemOne", map[string]any{}, map[string]any{"data": responseBody})
	require.NoError(t, err)
	encoded, err := common.Marshal(rendered)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "\"input_tokens\":296")
}

// Jev input pricing is $0.042 per million input tokens with free output, so
// every declared model needs a task usage expression that the typesafe schema
// can validate and that settles the measured input_tokens fact.
func TestBuiltInTypeSafeJevBuiltinBilling(t *testing.T) {
	plugin, _ := loadBuiltinPlugin(t, "typesafe")
	schema, _ := plugin.Meta.UsageForModel("jev-latest")
	require.Contains(t, schema, "input_tokens")

	for _, model := range plugin.Meta.Models {
		t.Run(model, func(t *testing.T) {
			expression, ok := billing_setting.GetBuiltinBillingExpr(model)
			require.True(t, ok, "built-in Jev price is missing")
			assert.True(t, billing_setting.TaskExprCompatible(expression, schema), "expression must match the typesafe usage schema")
			require.NoError(t, billing_setting.SmokeTestTaskExpr(expression, schema))

			result, err := billingexpr.ComputeTieredQuotaWithRequest(&billingexpr.BillingSnapshot{
				ExprString: expression, ExprHash: billingexpr.ExprHashString(expression),
				GroupRatio: 1, QuotaPerUnit: 500000, TaskUsageBilling: true,
			}, billingexpr.TokenParams{}, billingexpr.RequestInput{Usage: map[string]any{"input_tokens": float64(296)}})
			require.NoError(t, err)
			assert.Equal(t, 6, result.ActualQuotaAfterGroup)
		})
	}
}
