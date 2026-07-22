package controller

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestValidateTokenRoutingSettingsNormalizesDefaults(t *testing.T) {
	token := &model.Token{}
	require.NoError(t, validateTokenRoutingSettings(token))
	require.Equal(t, "auto", token.RouteMode)
	require.Equal(t, "priority", token.AutoRouteStrategy)
	require.Equal(t, 60, token.RateLimitWindow)
}

func TestValidateTokenRoutingSettingsRejectsUnsafeValues(t *testing.T) {
	tests := []model.Token{
		{MaxRatio: -1},
		{MaxRatio: math.NaN()},
		{MaxRatio: math.Inf(1)},
		{RateLimit: -1},
		{RateLimitWindow: 86401},
		{RouteMode: "invalid"},
	}
	for _, token := range tests {
		require.Error(t, validateTokenRoutingSettings(&token))
	}
}
