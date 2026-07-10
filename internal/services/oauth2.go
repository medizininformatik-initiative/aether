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

// oauth2CacheKey identifies a cached token by the credentials used to obtain it.
// Different issuers or clients must never share a token.
type oauth2CacheKey struct {
	issuerURI string
	clientID  string
}

type oauth2CacheEntry struct {
	token     string
	expiresAt time.Time
}

// oauth2TokenCache stores cached OAuth2 tokens per (issuerURI, clientID) to avoid
// fetching a new token for every request. Thread-safe for concurrent access.
var oauth2TokenCache = struct {
	sync.RWMutex
	entries map[oauth2CacheKey]oauth2CacheEntry
}{entries: make(map[oauth2CacheKey]oauth2CacheEntry)}

// oauth2TokenResponse represents the response from an OAuth2 token endpoint
type oauth2TokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"` // Token lifetime in seconds
	TokenType   string `json:"token_type"`
}

// FetchOAuth2Token retrieves an access token using the OAuth2 client credentials flow.
// Uses the Keycloak standard token endpoint: {issuer_uri}/protocol/openid-connect/token
func FetchOAuth2Token(issuerURI, clientID, clientSecret string, httpClient *HTTPClient) (string, error) {
	cacheKey := oauth2CacheKey{issuerURI: issuerURI, clientID: clientID}

	// Check cache first
	oauth2TokenCache.RLock()
	if entry, ok := oauth2TokenCache.entries[cacheKey]; ok && entry.token != "" && time.Now().Before(entry.expiresAt) {
		oauth2TokenCache.RUnlock()
		return entry.token, nil
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
	entry := oauth2CacheEntry{token: tokenResp.AccessToken}
	if tokenResp.ExpiresIn > 30 {
		entry.expiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn-30) * time.Second)
	} else {
		// Token expires very soon, don't cache
		entry.expiresAt = time.Now()
	}
	oauth2TokenCache.Lock()
	oauth2TokenCache.entries[cacheKey] = entry
	oauth2TokenCache.Unlock()

	return tokenResp.AccessToken, nil
}

// ClearOAuth2TokenCache clears all cached OAuth2 tokens (useful for testing)
func ClearOAuth2TokenCache() {
	oauth2TokenCache.Lock()
	oauth2TokenCache.entries = make(map[oauth2CacheKey]oauth2CacheEntry)
	oauth2TokenCache.Unlock()
}
