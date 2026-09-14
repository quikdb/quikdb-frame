package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	apiBase     = "https://device.quikdb.net"
	computeBase = "https://compute.quikdb.com"
	configDir   = ".quikdb-frame"
	configFile  = "auth.json"
)

type AuthConfig struct {
	Token            string `json:"token"`
	RefreshToken     string `json:"refreshToken,omitempty"`
	Email            string `json:"email,omitempty"`
	WalletAddress    string `json:"walletAddress,omitempty"`
	ExpiresAt        string `json:"expiresAt,omitempty"`
	SessionExpiresAt string `json:"sessionExpiresAt,omitempty"`
}

type authClient struct {
	BaseURL string
	HTTP    *http.Client
}

var authClientFactory = func() *authClient { return &authClient{apiBase, &http.Client{Timeout: 30 * time.Second}} }

type authAPIError struct {
	Status int
	Code   string
}

func (e *authAPIError) Error() string {
	return fmt.Sprintf("CLI authentication (%d): %s", e.Status, e.Code)
}

func (c *authClient) request(ctx context.Context, method, path, token string, payload, result interface{}) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+"/api/auth/cli/"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("CLI authentication request failed: %w", err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 65537))
	if err != nil {
		return err
	}
	if len(raw) > 65536 {
		return fmt.Errorf("authentication response too large")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if envelope.Error == "" {
			envelope.Error = http.StatusText(response.StatusCode)
		}
		return &authAPIError{response.StatusCode, envelope.Error}
	}
	if decodeErr != nil {
		return fmt.Errorf("invalid authentication response: %w", decodeErr)
	}
	if !envelope.Success || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return fmt.Errorf("authentication response did not confirm success")
	}
	if result != nil {
		return json.Unmarshal(envelope.Data, result)
	}
	return nil
}

type authProfile struct {
	Email           string `json:"email"`
	WalletAddress   string `json:"walletAddress"`
	Tier            string `json:"tier"`
	AccessExpiresAt string `json:"accessExpiresAt"`
}

func LoginWithToken(token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var profile authProfile
	if err := authClientFactory().request(ctx, "GET", "me", token, nil, &profile); err != nil {
		return err
	}
	expires, expiryErr := time.Parse(time.RFC3339, profile.AccessExpiresAt)
	if profile.WalletAddress == "" || expiryErr != nil || time.Until(expires) <= 0 {
		return fmt.Errorf("authentication response missing account identity")
	}
	if err := SaveAuth(&AuthConfig{Token: token, Email: profile.Email, WalletAddress: profile.WalletAddress, ExpiresAt: profile.AccessExpiresAt}); err != nil {
		return err
	}
	fmt.Println("Verified token saved.")
	return nil
}

func Whoami() error {
	token, err := RequireAuth()
	if err != nil {
		return err
	}
	var profile authProfile
	if err := authClientFactory().request(context.Background(), "GET", "me", token, nil, &profile); err != nil {
		return err
	}
	fmt.Printf("%s (%s), plan: %s\n", profile.Email, profile.WalletAddress, profile.Tier)
	return nil
}

func RequireAuth() (string, error) {
	if token := os.Getenv("QUIKDB_TOKEN"); token != "" {
		var profile authProfile
		if err := authClientFactory().request(context.Background(), "GET", "me", token, nil, &profile); err != nil {
			return "", err
		}
		if profile.WalletAddress == "" {
			return "", fmt.Errorf("authentication response missing account identity")
		}
		return token, nil
	}
	auth, err := LoadAuth()
	if err != nil || auth.Token == "" {
		return "", fmt.Errorf("not logged in; run quikdb-frame login")
	}
	if tokenCurrent(auth) {
		return auth.Token, nil
	}
	if auth.RefreshToken == "" {
		return "", fmt.Errorf("saved token has expired; run quikdb-frame login")
	}
	return refreshAuth()
}

func tokenCurrent(auth *AuthConfig) bool {
	expires, err := time.Parse(time.RFC3339, auth.ExpiresAt)
	return err == nil && time.Until(expires) > time.Minute
}

type tokenResponse struct {
	AccessToken      string `json:"accessToken"`
	RefreshToken     string `json:"refreshToken"`
	ExpiresIn        int    `json:"expiresIn"`
	SessionExpiresAt string `json:"sessionExpiresAt"`
	AccessExpiresAt  string `json:"accessExpiresAt"`
	WalletAddress    string `json:"walletAddress"`
	Email            string `json:"email"`
}

func saveTokens(tokens tokenResponse) error {
	expiry, expiryErr := time.Parse(time.RFC3339, tokens.AccessExpiresAt)
	if tokens.AccessToken == "" || tokens.RefreshToken == "" || tokens.WalletAddress == "" || tokens.ExpiresIn <= 0 || expiryErr != nil || time.Until(expiry) <= 0 {
		return fmt.Errorf("authentication response missing valid session credentials")
	}
	return SaveAuth(&AuthConfig{
		Token: tokens.AccessToken, RefreshToken: tokens.RefreshToken, Email: tokens.Email, WalletAddress: tokens.WalletAddress,
		ExpiresAt: tokens.AccessExpiresAt, SessionExpiresAt: tokens.SessionExpiresAt,
	})
}

func Logout() error {
	auth, _ := LoadAuth()
	var revokeErr error
	if auth != nil && auth.RefreshToken != "" {
		revokeErr = authClientFactory().request(context.Background(), "POST", "logout", "", map[string]string{"refreshToken": auth.RefreshToken}, nil)
	}
	if err := DeleteAuth(); err != nil {
		return fmt.Errorf("could not clear credentials: %w", err)
	}
	if revokeErr != nil {
		return fmt.Errorf("local credentials cleared, but server revocation failed: %w", revokeErr)
	}
	fmt.Println("Logged out.")
	return nil
}
