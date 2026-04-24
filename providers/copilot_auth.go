package providers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	copilotClientID       = "Iv1.b507a08c87ecfe98"
	copilotDeviceCodeURL  = "https://github.com/login/device/code"
	copilotAccessTokenURL = "https://github.com/login/oauth/access_token"
	copilotAPIKeyURL      = "https://api.github.com/copilot_internal/v2/token"
)

// CopilotTokenDir returns the directory for Copilot token storage (shared with LiteLLM).
func CopilotTokenDir() string {
	dir := os.Getenv("GITHUB_COPILOT_TOKEN_DIR")
	if dir != "" {
		home, _ := os.UserHomeDir()
		dir = strings.ReplaceAll(dir, "~", home)
		return filepath.Clean(dir)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "litellm", "github_copilot")
}

// CopilotAPIKeyFile matches ~/.config/litellm/github_copilot/api-key.json (LiteLLM format).
type CopilotAPIKeyFile struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
	Endpoints struct {
		API string `json:"api"`
	} `json:"endpoints"`
}

// LoadCopilotAPIKey reads the API key from api-key.json. Returns empty if expired or missing.
// If expired, attempts to refresh using access_token from access-token file.
func LoadCopilotAPIKey() (apiKey, apiBase string, err error) {
	dir := CopilotTokenDir()
	apiKeyPath := filepath.Join(dir, getEnv("GITHUB_COPILOT_API_KEY_FILE", "api-key.json"))
	accessPath := filepath.Join(dir, getEnv("GITHUB_COPILOT_ACCESS_TOKEN_FILE", "access-token"))

	data, err := os.ReadFile(apiKeyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var keyFile CopilotAPIKeyFile
	if err := json.Unmarshal(data, &keyFile); err != nil {
		return "", "", err
	}
	if keyFile.Token == "" {
		return "", "", nil
	}
	// Check expiry (with 5 min leeway)
	if keyFile.ExpiresAt > 0 && keyFile.ExpiresAt < time.Now().Unix()+300 {
		// Try refresh
		accessToken, _ := os.ReadFile(accessPath)
		tok := strings.TrimSpace(string(accessToken))
		if tok != "" {
			refreshed, e := refreshCopilotAPIKey(tok)
			if e == nil && refreshed != nil && refreshed.Token != "" {
				_ = saveCopilotAPIKeyFile(apiKeyPath, refreshed)
				return refreshed.Token, refreshed.Endpoints.API, nil
			}
		}
		return "", "", nil
	}
	apiBase = keyFile.Endpoints.API
	if apiBase == "" {
		apiBase = "https://api.githubcopilot.com"
	}
	return keyFile.Token, apiBase, nil
}

func getEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func refreshCopilotAPIKey(accessToken string) (*CopilotAPIKeyFile, error) {
	req, _ := http.NewRequest(http.MethodGet, copilotAPIKeyURL, nil)
	req.Header.Set("Authorization", "token "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Editor-Version", "vscode/1.85.1")
	req.Header.Set("Editor-Plugin-Version", "copilot/1.155.0")
	req.Header.Set("User-Agent", "GithubCopilot/1.155.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh api key: %s", resp.Status)
	}
	var keyFile CopilotAPIKeyFile
	if err := json.NewDecoder(resp.Body).Decode(&keyFile); err != nil {
		return nil, err
	}
	if keyFile.Token == "" {
		return nil, fmt.Errorf("refresh response missing token")
	}
	if keyFile.Endpoints.API == "" {
		keyFile.Endpoints.API = "https://api.githubcopilot.com"
	}
	if keyFile.ExpiresAt == 0 {
		keyFile.ExpiresAt = time.Now().Unix() + 3600*8
	}
	return &keyFile, nil
}

func saveCopilotAPIKeyFile(path string, keyFile *CopilotAPIKeyFile) error {
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	data, _ := json.MarshalIndent(keyFile, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

// RunCopilotDeviceFlow performs OAuth device flow and saves tokens.
func RunCopilotDeviceFlow() error {
	body, _ := json.Marshal(map[string]string{"client_id": copilotClientID, "scope": "read:user"})
	req, _ := http.NewRequest(http.MethodPost, copilotDeviceCodeURL, bytes.NewReader(body))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("device code: %s", resp.Status)
	}
	var codeResp struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&codeResp); err != nil {
		return err
	}
	if codeResp.DeviceCode == "" || codeResp.UserCode == "" {
		return fmt.Errorf("device code response missing fields")
	}
	uri := codeResp.VerificationURI
	if uri == "" {
		uri = "https://github.com/login/device"
	}
	interval := codeResp.Interval
	if interval < 5 {
		interval = 5
	}

	fmt.Printf("\nPlease visit %s and enter code: %s\n\n", uri, codeResp.UserCode)

	// Poll for access token
	for i := 0; i < 24; i++ {
		time.Sleep(time.Duration(interval) * time.Second)

		pollBody, _ := json.Marshal(map[string]string{
			"client_id":   copilotClientID,
			"device_code": codeResp.DeviceCode,
			"grant_type":  "urn:ietf:params:oauth:grant-type:device_code",
		})
		pollReq, _ := http.NewRequest(http.MethodPost, copilotAccessTokenURL, bytes.NewReader(pollBody))
		pollReq.Header.Set("Accept", "application/json")
		pollReq.Header.Set("Content-Type", "application/json")

		resp, err = http.DefaultClient.Do(pollReq)
		if err != nil {
			continue
		}
		var tokResp struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&tokResp)
		resp.Body.Close()

		if tokResp.AccessToken != "" {
			dir := CopilotTokenDir()
			_ = os.MkdirAll(dir, 0o700)
			accessPath := filepath.Join(dir, getEnv("GITHUB_COPILOT_ACCESS_TOKEN_FILE", "access-token"))
			if err := os.WriteFile(accessPath, []byte(tokResp.AccessToken), 0o600); err != nil {
				return err
			}
			keyFile, err := refreshCopilotAPIKey(tokResp.AccessToken)
			if err != nil {
				return fmt.Errorf("refresh api key: %w", err)
			}
			apiKeyPath := filepath.Join(dir, getEnv("GITHUB_COPILOT_API_KEY_FILE", "api-key.json"))
			if err := saveCopilotAPIKeyFile(apiKeyPath, keyFile); err != nil {
				return err
			}
			return nil
		}
		if tokResp.Error != "authorization_pending" {
			return fmt.Errorf("auth failed: %s", tokResp.Error)
		}
	}
	return fmt.Errorf("timed out waiting for authorization")
}
