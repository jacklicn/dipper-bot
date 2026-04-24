package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// CodexAuthFile matches ~/.codex/auth.json structure (shared with Codex CLI and oauth-cli-kit).
type CodexAuthFile struct {
	AuthMode   string `json:"auth_mode"`
	AccountID  string `json:"account_id"`
	LastRefresh int64 `json:"last_refresh,omitempty"`
	Tokens     struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	} `json:"tokens"`
}

// LoadCodexAuth reads ~/.codex/auth.json and returns (accessToken, accountID, error).
// Returns empty strings if file is missing or invalid.
func LoadCodexAuth() (accessToken, accountID string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	path := filepath.Join(home, ".codex", "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	var auth CodexAuthFile
	if err := json.Unmarshal(data, &auth); err != nil {
		return "", "", err
	}
	return auth.Tokens.AccessToken, auth.AccountID, nil
}
