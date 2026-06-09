package cli

import (
	"testing"
	"time"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func saveTestUserLogin(t *testing.T, apiURL string) {
	t.Helper()
	t.Setenv("DARI_API_KEY", "")
	t.Setenv("DARI_ORG_ID", "")
	t.Setenv("DARI_CONFIG_DIR", t.TempDir())
	expiresAt := time.Now().Add(time.Hour).Unix()
	if err := state.Save(&state.CliState{
		APIURL: api.NormalizeURL(apiURL),
		SupabaseSession: &state.SupabaseSession{
			AccessToken:  "jwt_test",
			RefreshToken: "refresh_test",
			ExpiresAt:    &expiresAt,
			UserID:       "user_123",
			Email:        "test@example.com",
		},
		CurrentOrgID: "org_123",
		Organizations: map[string]state.Organization{
			"org_123": {ID: "org_123", Name: "Test Org", Slug: "test-org", Role: "owner"},
		},
	}); err != nil {
		t.Fatalf("save state: %v", err)
	}
}
