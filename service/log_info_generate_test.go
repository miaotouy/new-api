package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAppendRequestPathRecordsInboundAndUpstreamPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newRequestContext := func(target string) *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest("POST", target, nil)
		return ctx
	}

	t.Run("endpoint conversion keeps both paths", func(t *testing.T) {
		other := model.NewLogOther()
		AppendRequestPath(
			newRequestContext("/v1/chat/completions"),
			&relaycommon.RelayInfo{UpstreamURLPath: "/v1/responses"},
			other,
		)

		snapshot := other.Snapshot()
		assert.Equal(t, "/v1/chat/completions", snapshot["request_path"])
		assert.Equal(t, "/v1/responses", snapshot["upstream_request_path"])
	})

	t.Run("unchanged endpoint stores the same path twice", func(t *testing.T) {
		other := model.NewLogOther()
		AppendRequestPath(
			newRequestContext("/v1/responses"),
			&relaycommon.RelayInfo{UpstreamURLPath: "/v1/responses"},
			other,
		)

		snapshot := other.Snapshot()
		assert.Equal(t, "/v1/responses", snapshot["request_path"])
		assert.Equal(t, "/v1/responses", snapshot["upstream_request_path"])
	})

	t.Run("no upstream attempt leaves the upstream path unset", func(t *testing.T) {
		other := model.NewLogOther()
		AppendRequestPath(newRequestContext("/v1/chat/completions"), &relaycommon.RelayInfo{}, other)

		snapshot := other.Snapshot()
		assert.Equal(t, "/v1/chat/completions", snapshot["request_path"])
		assert.NotContains(t, snapshot, "upstream_request_path")
	})

	t.Run("fallback path strips the query string", func(t *testing.T) {
		other := model.NewLogOther()
		AppendRequestPath(
			nil,
			&relaycommon.RelayInfo{
				RequestURLPath:  "/v1/responses?beta=true",
				UpstreamURLPath: "/v1/responses",
			},
			other,
		)

		snapshot := other.Snapshot()
		assert.Equal(t, "/v1/responses", snapshot["request_path"])
		assert.Equal(t, "/v1/responses", snapshot["upstream_request_path"])
	})
}
