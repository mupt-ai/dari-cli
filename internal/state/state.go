// Package state loads and persists the CLI's local auth + organization state.
// The on-disk format is intentionally identical to the Python CLI's so that
// users can upgrade from pip to Homebrew without re-authenticating.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

const (
	SchemaVersion = 1
	Filename      = "state.json"
)

type SupabaseSession struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    *int64 `json:"expires_at"`
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name,omitempty"`
}

type Organization struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Role   string `json:"role"`
	APIKey string `json:"api_key,omitempty"`
}

type CliState struct {
	APIURL          string                  `json:"api_url,omitempty"`
	SupabaseSession *SupabaseSession        `json:"supabase_session"`
	CurrentOrgID    string                  `json:"current_org_id,omitempty"`
	Organizations   map[string]Organization `json:"organizations"`
}

type onDisk struct {
	SchemaVersion   int                     `json:"schema_version"`
	APIURL          *string                 `json:"api_url"`
	SupabaseSession *SupabaseSession        `json:"supabase_session"`
	CurrentOrgID    *string                 `json:"current_org_id"`
	Organizations   map[string]Organization `json:"organizations"`
}

// Load reads the CLI state from disk. Returns an empty state when the file
// does not exist.
func Load() (*CliState, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &CliState{Organizations: map[string]Organization{}}, nil
		}
		return nil, fmt.Errorf("read state: %w", err)
	}
	var raw onDisk
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	if raw.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("unsupported dari CLI state schema version: %d", raw.SchemaVersion)
	}
	s := &CliState{
		SupabaseSession: raw.SupabaseSession,
		Organizations:   raw.Organizations,
	}
	if raw.APIURL != nil {
		s.APIURL = *raw.APIURL
	}
	if raw.CurrentOrgID != nil {
		s.CurrentOrgID = *raw.CurrentOrgID
	}
	if s.Organizations == nil {
		s.Organizations = map[string]Organization{}
	}
	return s, nil
}

// Save persists the CLI state with user-only permissions on Unix.
func Save(s *CliState) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(s.marshalShape(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	perm := os.FileMode(0o644)
	if runtime.GOOS != "windows" {
		perm = 0o600
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// Clear removes the on-disk state, if any.
func Clear() error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove state: %w", err)
	}
	return nil
}

// Path resolves the CLI state file path, honoring DARI_CONFIG_DIR, then
// XDG_CONFIG_HOME, then APPDATA (Windows), falling back to ~/.config/dari.
func Path() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("DARI_CONFIG_DIR")); dir != "" {
		expanded, err := expandHome(dir)
		if err != nil {
			return "", err
		}
		return filepath.Join(expanded, Filename), nil
	}
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		expanded, err := expandHome(dir)
		if err != nil {
			return "", err
		}
		return filepath.Join(expanded, "dari", Filename), nil
	}
	if dir := strings.TrimSpace(os.Getenv("APPDATA")); dir != "" {
		return filepath.Join(dir, "dari", Filename), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".config", "dari", Filename), nil
}

func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~")), nil
}

// marshalShape produces a sorted, deterministic on-disk representation
// matching the Python CLI's output byte-for-byte (modulo whitespace).
func (s *CliState) marshalShape() onDisk {
	ids := make([]string, 0, len(s.Organizations))
	for id := range s.Organizations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	orgs := make(map[string]Organization, len(ids))
	for _, id := range ids {
		orgs[id] = s.Organizations[id]
	}

	shape := onDisk{
		SchemaVersion:   SchemaVersion,
		SupabaseSession: s.SupabaseSession,
		Organizations:   orgs,
	}
	if s.APIURL != "" {
		v := s.APIURL
		shape.APIURL = &v
	}
	if s.CurrentOrgID != "" {
		v := s.CurrentOrgID
		shape.CurrentOrgID = &v
	}
	return shape
}

// CurrentOrg returns the currently-selected organization, or nil if none.
func (s *CliState) CurrentOrg() *Organization {
	if s.CurrentOrgID == "" {
		return nil
	}
	org, ok := s.Organizations[s.CurrentOrgID]
	if !ok {
		return nil
	}
	return &org
}
