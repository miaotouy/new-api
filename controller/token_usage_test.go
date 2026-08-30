package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGetUserTokenUsageDataRejectsRangesOverOneMonth(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("id", 1)
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/data/tokens/self?start_timestamp=1&end_timestamp=2592002",
		nil,
	)

	GetUserTokenUsageData(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.False(t, payload.Success)
	require.Equal(t, "时间跨度不能超过 1 个月", payload.Message)
}
