package oauth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Credentials struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	IDToken          string `json:"id_token,omitempty"`
	ExpiresAt        int64  `json:"expires_at"`
	ClientID         string `json:"client_id,omitempty"`
	Email            string `json:"email,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	ChatGPTUserID    string `json:"chatgpt_user_id,omitempty"`
	OrganizationID   string `json:"organization_id,omitempty"`
	PlanType         string `json:"plan_type,omitempty"`
	UpdatedAt        int64  `json:"updated_at,omitempty"`
}

type Store struct {
	path string
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Save(cred *Credentials) error {
	if cred == nil {
		return errors.New("credentials are required")
	}
	cred.UpdatedAt = time.Now().Unix()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cred, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.path)
}

func (s *Store) Load() (*Credentials, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	var cred Credentials
	if err := json.Unmarshal(data, &cred); err != nil {
		return nil, err
	}
	return &cred, nil
}
