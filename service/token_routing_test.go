package service

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newTokenRoutingTestContext() *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	return ctx
}

func TestShouldUseTokenRoutingKeepsLegacyCrossGroupRetry(t *testing.T) {
	ctx := newTokenRoutingTestContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenCrossGroupRetry, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenFailoverEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenRouteMode, "")
	common.SetContextKey(ctx, constant.ContextKeyTokenAutoRouteStrategy, "")

	require.False(t, ShouldUseTokenRouting(ctx))
}

func TestTokenRoutingModeControlsRetryPolicy(t *testing.T) {
	ctx := newTokenRoutingTestContext()
	common.SetContextKey(ctx, constant.ContextKeyTokenRoutingConfigured, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenRouteMode, "manual")
	common.SetContextKey(ctx, constant.ContextKeyTokenFailoverEnabled, false)

	require.True(t, ShouldUseTokenRouting(ctx))
	require.True(t, ShouldSkipTokenRouteRetry(ctx))

	common.SetContextKey(ctx, constant.ContextKeyTokenFailoverEnabled, true)
	require.False(t, ShouldSkipTokenRouteRetry(ctx))
}
