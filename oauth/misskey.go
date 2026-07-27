package oauth

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func init() {
	Register("misskey", &MisskeyProvider{})
}

// MisskeyProvider implements OAuth for Misskey/Sharkey instances via MiAuth
type MisskeyProvider struct{}

type misskeyTokenCheckResponse struct {
	Ok    bool   `json:"ok"`
	Token string `json:"token"`
}

type misskeyUserInfo struct {
	Id       string `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
}

func (p *MisskeyProvider) GetName() string {
	return "Misskey"
}

func (p *MisskeyProvider) IsEnabled() bool {
	return common.MisskeyOAuthEnabled && common.MisskeyInstance != ""
}

func (p *MisskeyProvider) getInstance() string {
	return strings.TrimRight(common.MisskeyInstance, "/")
}

func (p *MisskeyProvider) ExchangeToken(ctx context.Context, code string, c *gin.Context) (*OAuthToken, error) {
	if code == "" {
		return nil, NewOAuthError(i18n.MsgOAuthInvalidCode, nil)
	}

	instance := p.getInstance()
	logger.LogDebug(ctx, "[OAuth-Misskey] ExchangeToken: code=%s..., instance=%s", code[:min(len(code), 10)], instance)

	// MiAuth: code is the session ID, check it against /api/miauth/{session}/check
	checkURL := fmt.Sprintf("%s/api/miauth/%s/check", instance, code)

	body := bytes.NewBuffer([]byte("{}"))
	req, err := http.NewRequestWithContext(ctx, "POST", checkURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NewAPI-Misskey/1.0")

	client := http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] ExchangeToken error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Misskey"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Misskey] ExchangeToken response status: %d", res.StatusCode)

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var checkRes misskeyTokenCheckResponse
	if err := common.Unmarshal(respBody, &checkRes); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] ExchangeToken decode error: %s", err.Error()))
		return nil, err
	}

	if !checkRes.Ok || checkRes.Token == "" {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] ExchangeToken failed: ok=%v, has_token=%v", checkRes.Ok, checkRes.Token != ""))
		return nil, NewOAuthError(i18n.MsgOAuthTokenFailed, map[string]any{"Provider": "Misskey"})
	}

	logger.LogDebug(ctx, "[OAuth-Misskey] ExchangeToken success")

	return &OAuthToken{
		AccessToken: checkRes.Token,
		TokenType:   "Bearer",
	}, nil
}

func (p *MisskeyProvider) GetUserInfo(ctx context.Context, token *OAuthToken) (*OAuthUser, error) {
	instance := p.getInstance()
	logger.LogDebug(ctx, "[OAuth-Misskey] GetUserInfo: fetching user info from %s", instance)

	// Call /api/i with the token
	bodyData := map[string]string{"i": token.AccessToken}
	jsonBody, err := common.Marshal(bodyData)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", instance+"/api/i", bytes.NewBuffer(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NewAPI-Misskey/1.0")

	client := http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] GetUserInfo error: %s", err.Error()))
		return nil, NewOAuthErrorWithRaw(i18n.MsgOAuthConnectFailed, map[string]any{"Provider": "Misskey"}, err.Error())
	}
	defer res.Body.Close()

	logger.LogDebug(ctx, "[OAuth-Misskey] GetUserInfo response status: %d", res.StatusCode)

	if res.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] GetUserInfo failed: status=%d", res.StatusCode))
		return nil, NewOAuthError(i18n.MsgOAuthGetUserErr, map[string]any{"Provider": "Misskey"})
	}

	respBody, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	var userInfo misskeyUserInfo
	if err := common.Unmarshal(respBody, &userInfo); err != nil {
		logger.LogError(ctx, fmt.Sprintf("[OAuth-Misskey] GetUserInfo decode error: %s", err.Error()))
		return nil, err
	}

	if userInfo.Id == "" {
		logger.LogError(ctx, "[OAuth-Misskey] GetUserInfo failed: empty user id")
		return nil, NewOAuthError(i18n.MsgOAuthUserInfoEmpty, map[string]any{"Provider": "Misskey"})
	}

	// Use username as display name if name is empty
	displayName := userInfo.Name
	if displayName == "" {
		displayName = userInfo.Username
	}

	logger.LogDebug(ctx, "[OAuth-Misskey] GetUserInfo success: id=%s, username=%s, name=%s",
		userInfo.Id, userInfo.Username, userInfo.Name)

	return &OAuthUser{
		ProviderUserID: userInfo.Id,
		Username:       userInfo.Username,
		DisplayName:    displayName,
		Extra: map[string]any{
			"instance": instance,
		},
	}, nil
}

func (p *MisskeyProvider) IsUserIDTaken(providerUserID string) bool {
	return model.IsMisskeyIdAlreadyTaken(providerUserID)
}

func (p *MisskeyProvider) FillUserByProviderID(user *model.User, providerUserID string) error {
	user.MisskeyId = providerUserID
	return user.FillUserByMisskeyId()
}

func (p *MisskeyProvider) SetProviderUserID(user *model.User, providerUserID string) {
	user.MisskeyId = providerUserID
}

func (p *MisskeyProvider) GetProviderPrefix() string {
	return "misskey_"
}

// GetAuthorizeURL generates the Misskey MiAuth authorization URL
// This is called before the OAuth redirect to generate the proper MiAuth URL
func (p *MisskeyProvider) GetAuthorizeURL(ctx context.Context, c *gin.Context) (string, error) {
	cfg := GetOAuthConfig(p)
	if cfg == nil {
		return "", fmt.Errorf("misskey auth config not found")
	}

	instance := p.getInstance()

	// Build the callback URL (New API's own OAuth callback)
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	callbackURL := fmt.Sprintf("%s://%s/api/oauth/misskey", scheme, c.Request.Host)

	// Generate a random session ID
	sessionId := common.GetRandomString(32)

	// Build MiAuth URL
	authURL := fmt.Sprintf("%s/miauth/%s", instance, sessionId)

	params := url.Values{}
	params.Set("name", "NewAPI%E7%99%BB%E5%BD%95") // "NewAPI登录" URL-encoded
	params.Set("callback", callbackURL+"?code="+sessionId)
	params.Set("permission", "read:account")

	return authURL + "?" + params.Encode(), nil
}

// GetOAuthConfig holds the OAuth configuration for generating authorize URLs
type OAuthConfig struct {
	ClientId    string
	RedirectURL string
	Scopes      []string
}

// GetOAuthConfig returns nil for Misskey since it uses MiAuth, not standard OAuth2
// This is used by the frontend to determine if standard OAuth2 flow should be used
func GetOAuthConfig(provider Provider) *OAuthConfig {
	if mp, ok := provider.(*MisskeyProvider); ok {
		_ = mp // Misskey doesn't use standard OAuth config
		return nil
	}
	return nil
}
