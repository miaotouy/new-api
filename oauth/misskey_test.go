package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMisskeyExchangeToken(t *testing.T) {
	oldInstance := common.MisskeyInstance
	t.Cleanup(func() { common.MisskeyInstance = oldInstance })
	provider := &MisskeyProvider{}

	for _, tc := range []struct {
		name     string
		code     string
		response string
		wantKey  string
	}{
		{name: "empty code", wantKey: "Invalid authorization code"},
		{name: "rejected session", code: "session-id", response: `{"ok":false}`, wantKey: "Failed to get token from {{provider}}, please check settings"},
		{name: "missing token", code: "session-id", response: `{"ok":true}`, wantKey: "Failed to get token from {{provider}}, please check settings"},
		{name: "valid session", code: "session-id", response: `{"ok":true,"token":"access-token"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodPost, r.Method)
				assert.Equal(t, "/api/miauth/session-id/check", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.response))
			}))
			defer server.Close()
			common.MisskeyInstance = server.URL
			token, err := provider.ExchangeToken(context.Background(), tc.code, nil)
			if tc.wantKey == "" {
				require.NoError(t, err)
				require.NotNil(t, token)
				assert.Equal(t, "access-token", token.AccessToken)
				assert.Equal(t, "Bearer", token.TokenType)
				return
			}
			require.Error(t, err)
			assert.Nil(t, token)
			var oauthErr *OAuthError
			require.ErrorAs(t, err, &oauthErr)
			assert.Equal(t, tc.wantKey, oauthErr.Message.Key)
			if tc.code != "" {
				assert.Equal(t, "Misskey", oauthErr.Message.Params["provider"])
			}
		})
	}

	t.Run("transport error excludes the authorization session", func(t *testing.T) {
		common.MisskeyInstance = "https://example.invalid"
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		token, err := provider.ExchangeToken(ctx, "secret-miauth-session", nil)
		require.Error(t, err)
		assert.Nil(t, token)
		var oauthErr *OAuthError
		require.ErrorAs(t, err, &oauthErr)
		assert.Equal(t, "Unable to connect to {{provider}} server, please try again later", oauthErr.Message.Key)
		assert.Equal(t, "Misskey", oauthErr.Message.Params["provider"])
		assert.Equal(t, context.Canceled.Error(), oauthErr.RawError)
	})
}
