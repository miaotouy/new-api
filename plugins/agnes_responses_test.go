package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadAgnesPlugin(t *testing.T) *jsplugin.LoadedPlugin {
	t.Helper()
	source, err := builtinplugins.Source("agnes")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "agnes"})
	require.NoError(t, err)
	return plugin
}

func callAgnesHook(t *testing.T, plugin *jsplugin.LoadedPlugin, name string, args ...any) map[string]any {
	t.Helper()
	value, err := plugin.Engine.Call(t.Context(), name, args...)
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, common.Unmarshal(encoded, &result))
	return result
}

func TestAgnesResponsesProtocol(t *testing.T) {
	testVideoResponsesProtocol(t, videoResponsesTestCase{
		pluginKey: "agnes",
		model:     "agnes-video-2.5",
		requestBody: map[string]any{
			"model": "agnes-video-2.5",
			"input": []any{map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "input_text", "text": "a cat walking through rain"},
				map[string]any{"type": "input_image", "image_url": "https://cdn.example/cat.png"},
			}}},
			"seconds": 8,
			"size":    "2K",
		},
		wantAction: "reference_to_video",
		wantRequest: map[string]any{
			"model":   "agnes-video-2.5",
			"prompt":  "a cat walking through rain",
			"seconds": "8",
			"size":    "2K",
			"mode":    "reference",
			"images":  []any{"https://cdn.example/cat.png"},
		},
		wantUsageKeys:  []string{"input_images", "seconds", "size"},
		wantVendorName: "agnes",
	})
}

func TestAgnesSubmitAndQueryUseVideoID(t *testing.T) {
	plugin := loadAgnesPlugin(t)
	request := map[string]any{
		"model":   "agnes-video-2.5",
		"prompt":  "a cat walking through rain",
		"seconds": 8,
		"size":    "720P",
		"mode":    "text",
	}
	descriptor := callAgnesHook(t, plugin, "buildSubmitRequest", map[string]any{
		"baseUrl":       "https://apihub.agnes-ai.com/v1",
		"apiKey":        "agnes-key",
		"upstreamModel": "agnes-video-2.5",
		"requestBody":   request,
	})
	assert.Equal(t, "https://apihub.agnes-ai.com/v1/videos", descriptor["url"])
	assert.Equal(t, "POST", descriptor["method"])
	body, err := common.Marshal(descriptor["body"])
	require.NoError(t, err)
	assert.JSONEq(t, `{"model":"agnes-video-2.5","prompt":"a cat walking through rain","seconds":"8","mode":"text","size":"720P"}`, string(body))

	parsed := callAgnesHook(t, plugin, "parseSubmitResponse", map[string]any{}, map[string]any{
		"body": map[string]any{"id": "task_1", "task_id": "task_1", "video_id": "video_1", "status": "queued"},
	})
	assert.Equal(t, "task_1", parsed["taskId"])
	state, ok := parsed["state"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "video_1", state["video_id"])

	query := callAgnesHook(t, plugin, "buildQueryRequest", map[string]any{
		"baseUrl":       "https://apihub.agnes-ai.com/v1",
		"apiKey":        "agnes-key",
		"model":         "agnes-video-2.5",
		"upstreamModel": "agnes-video-2.5",
		"taskId":        "task_1",
		"state":         state,
	})
	assert.Equal(t, "https://apihub.agnes-ai.com/agnesapi?video_id=video_1&model_name=agnes-video-2.5", query["url"])
	assert.Equal(t, "GET", query["method"])

	gatewaySubmit := callAgnesHook(t, plugin, "buildSubmitRequest", map[string]any{
		"baseUrl":       "https://gateway.example/v1",
		"apiKey":        "gateway-key",
		"authHeader":    "Bearer gateway-key",
		"upstream":      map[string]any{"kind": "new_api"},
		"model":         "agnes-video-2.5",
		"upstreamModel": "agnes-video-2.5",
		"requestBody":   request,
	})
	assert.Equal(t, "https://gateway.example/agnes/v1/videos", gatewaySubmit["url"])
	gatewayQuery := callAgnesHook(t, plugin, "buildQueryRequest", map[string]any{
		"baseUrl":       "https://gateway.example/v1",
		"apiKey":        "gateway-key",
		"authHeader":    "Bearer gateway-key",
		"upstream":      map[string]any{"kind": "new_api"},
		"model":         "agnes-video-2.5",
		"upstreamModel": "agnes-video-2.5",
		"taskId":        "video_1",
		"state":         map[string]any{"video_id": "video_1"},
	})
	assert.Equal(t, "https://gateway.example/agnes/agnesapi/video_1?model_name=agnes-video-2.5", gatewayQuery["url"])
}

func TestAgnesUsageAndValidation(t *testing.T) {
	plugin := loadAgnesPlugin(t)
	usage := callAgnesHook(t, plugin, "extractUsage", map[string]any{
		"requestBody": map[string]any{
			"prompt":  "reference",
			"seconds": 8,
			"size":    "720P",
			"images":  []any{"a", "b", "c", "d", "e", "f", "g"},
			"videos":  []any{map[string]any{"url": "video", "duration": 3}},
		},
	})
	assert.Equal(t, float64(8), usage["seconds"])
	assert.Equal(t, "720P", usage["size"])
	assert.Equal(t, float64(7), usage["input_images"])
	assert.NotContains(t, usage, "input_video_seconds")

	decoded, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
		"body":  map[string]any{"kind": "json", "value": map[string]any{"model": "agnes-video-2.5", "prompt": "reference", "videos": []any{map[string]any{"url": "https://cdn.example/in.mp4", "start_seconds": 1}}}},
		"model": "agnes-video-2.5",
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(decoded)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"submit","model":"agnes-video-2.5","action":"reference_to_video","requestBody":{"model":"agnes-video-2.5","prompt":"reference","seconds":"5","mode":"reference","size":"720P","videos":[{"url":"https://cdn.example/in.mp4","start_seconds":1}]}}`, string(encoded))

	completed := callAgnesHook(t, plugin, "extractUsageOnComplete", nil, nil, map[string]any{"seconds": "4", "size": "1K"})
	assert.Equal(t, float64(4), completed["seconds"])
	assert.Equal(t, "1K", completed["size"])

	_, err = plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "decodeRequest"}, map[string]any{
		"body":  map[string]any{"kind": "json", "value": map[string]any{"model": "agnes-video-2.5", "prompt": "too long", "seconds": 13}},
		"model": "agnes-video-2.5",
	})
	require.ErrorContains(t, err, "seconds must be between 4 and 12")
}

func TestAgnesReferenceVideoThroughResponses(t *testing.T) {
	plugin := loadAgnesPlugin(t)
	value, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_responses", "decodeRequest"}, map[string]any{
		"body": map[string]any{"kind": "json", "value": map[string]any{
			"model":  "agnes-video-2.5",
			"input":  "continue this clip",
			"videos": []any{map[string]any{"url": "https://cdn.example/in.mp4"}},
		}},
		"model": "agnes-video-2.5",
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"submit","model":"agnes-video-2.5","action":"reference_to_video","requestBody":{"model":"agnes-video-2.5","prompt":"continue this clip","seconds":"5","mode":"reference","size":"720P","videos":[{"url":"https://cdn.example/in.mp4"}]}}`, string(encoded))
}
func TestAgnesArtifactUsesMetadataURL(t *testing.T) {
	plugin := loadAgnesPlugin(t)
	value, err := plugin.Engine.Call(t.Context(), "listArtifacts", map[string]any{
		"status": "SUCCESS",
		"data":   map[string]any{"metadata": map[string]any{"url": "https://cdn.example/video.mp4"}},
	})
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var artifacts []map[string]any
	require.NoError(t, common.Unmarshal(encoded, &artifacts))
	require.Len(t, artifacts, 1)
	assert.Equal(t, "video", artifacts[0]["key"])

	content := callAgnesHook(t, plugin, "buildContentRequest", map[string]any{
		"artifactKey":   "video",
		"data":          map[string]any{"metadata": map[string]any{"url": "https://cdn.example/video.mp4"}},
		"clientRequest": map[string]any{"method": "GET"},
	})
	assert.Equal(t, "https://cdn.example/video.mp4", content["url"])
	assert.Equal(t, true, content["credentialless"])
}
