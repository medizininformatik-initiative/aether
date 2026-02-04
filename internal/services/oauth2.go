package services

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// oauth2TokenCache stores cached OAuth2 tokens to avoid fetching a new token for every request.
// Thread-safe for concurrent access.
var oauth2TokenCache = struct {
	sync.RWMutex
	token     string
	expiresAt time.Time
}{}

// oauth2TokenResponse represents the response from an OAuth2 token endpoint
type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // Token lifetime in seconds
	TokenType   string `json:"token_type"`
}

// FetchOAuth2Token retrieves an access token using the OAuth2 client credentials flow.
// Uses the Keycloak standard token endpoint: {issuer_uri}/protocol/openid-connect/token
func FetchOAuth2Token(issuerURI, clientID, clientSecret string, httpClient *HTTPClient) (string, error) {
	// Check cache first
	oauth2TokenCache.RLock()
	if oauth2TokenCache.token != "" && time.Now().Before(oauth2TokenCache.expiresAt) {
		token := oauth2TokenCache.token
		oauth2TokenCache.RUnlock()
		return token, nil
	}
	oauth2TokenCache.RUnlock()

	// Build token endpoint URL (Keycloak standard path)
	tokenURL := strings.TrimSuffix(issuerURI, "/") + "/protocol/openid-connect/token"

	// Build request body
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", clientID)
	data.Set("client_secret", clientSecret)

	// Create request
	req, err := http.NewRequest("POST", tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Execute request
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tokenResp oauth2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}

	// Cache the token with a small buffer before expiration (30 seconds)
	oauth2TokenCache.Lock()
	oauth2TokenCache.token = tokenResp.AccessToken
	if tokenResp.ExpiresIn > 30 {
		oauth2TokenCache.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-30) * time.Second)
	} else {
		// Token expires very soon, don't cache
		oauth2TokenCache.expiresAt = time.Now()
	}
	oauth2TokenCache.Unlock()

	return tokenResp.AccessToken, nil
}

// ClearOAuth2TokenCache clears the cached OAuth2 token (useful for testing)
func ClearOAuth2TokenCache() {
	oauth2TokenCache.Lock()
	oauth2TokenCache.token = ""
	oauth2TokenCache.expiresAt = time.Time{}
	oauth2TokenCache.Unlock()
}
