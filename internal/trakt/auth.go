package trakt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// RequestDeviceCode starts the device auth flow.
// Returns a DeviceCode containing the user_code the user must enter at the verification URL.
func (c *Client) RequestDeviceCode(ctx context.Context) (*DeviceCode, error) {
	resp, err := c.do(ctx, http.MethodPost, "/oauth/device/code", nil, struct {
		ClientID string `json:"client_id"`
	}{ClientID: c.clientID})
	if err != nil {
		return nil, fmt.Errorf("requesting device code: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("requesting device code: unexpected status %d", resp.StatusCode)
	}

	var dc DeviceCode
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, fmt.Errorf("decoding device code response: %w", err)
	}

	return &dc, nil
}

// PollForToken exchanges a device_code for OAuth tokens.
// Returns (nil, nil) if the user hasn't authorized yet (keep polling).
func (c *Client) PollForToken(ctx context.Context, deviceCode string) (*Token, error) {
	resp, err := c.do(ctx, http.MethodPost, "/oauth/device/token", nil, struct {
		DeviceCode   string `json:"code"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}{DeviceCode: deviceCode, ClientID: c.clientID, ClientSecret: c.clientSecret})
	if err != nil {
		return nil, fmt.Errorf("polling for token: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode == http.StatusBadRequest {
		return nil, nil
	}
	if resp.StatusCode == http.StatusGone {
		return nil, fmt.Errorf("polling for token: code expired")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("polling for token: unexpected status %d", resp.StatusCode)
	}

	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	return &t, nil
}

// RefreshToken exchanges a refresh token for a new access/refresh token pair.
// Uses: POST /oauth/token with grant_type "refresh_token".
// After a successful refresh, the OLD refresh token is invalidated — callers
// must save both the new AccessToken and new RefreshToken.
func (c *Client) RefreshToken(ctx context.Context, refreshToken string) (*Token, error) {
	resp, err := c.do(ctx, http.MethodPost, "/oauth/token", nil, struct {
		RefreshToken string `json:"refresh_token"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		RedirectURI  string `json:"redirect_uri"`
		GrantType    string `json:"grant_type"`
	}{
		RefreshToken: refreshToken,
		ClientID:     c.clientID,
		ClientSecret: c.clientSecret,
		RedirectURI:  "urn:ietf:wg:oauth:2.0:oob", // required by Trakt even for device flow apps
		GrantType:    "refresh_token",
	})
	if err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}
	defer closeBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refreshing token: unexpected status %d", resp.StatusCode)
	}

	var t Token
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("decoding refreshed token response: %w", err)
	}
	return &t, nil
}
