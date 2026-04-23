package auth

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

// OrgRecord is the over-the-wire shape the server uses for orgs.
type OrgRecord struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}

type bootstrapResponse struct {
	Organizations []OrgRecord `json:"organizations"`
}

type orgsListResponse struct {
	Organizations []OrgRecord `json:"organizations"`
}

type managedKeyResponse struct {
	APIKey string `json:"api_key"`
}

// bootstrapAndSelectOrg is called during Login to populate the org list and
// pick a current org (preferredOrgID wins if set, else the first). It also
// caches the managed CLI key for the selected org.
func bootstrapAndSelectOrg(ctx context.Context, s *state.CliState, apiURL, preferredOrgID string) error {
	if s.SupabaseSession == nil {
		return ErrNotLoggedIn
	}
	var resp bootstrapResponse
	client := api.New(apiURL).WithBearer(s.SupabaseSession.AccessToken)
	if err := client.Do(ctx, http.MethodPost, "/v1/me/bootstrap", map[string]any{}, &resp); err != nil {
		return fmt.Errorf("bootstrap user: %w", err)
	}
	syncOrganizations(s, resp.Organizations)

	selected := preferredOrgID
	if selected == "" {
		selected = s.CurrentOrgID
	}
	if _, ok := s.Organizations[selected]; !ok {
		if len(resp.Organizations) == 0 {
			return fmt.Errorf("no organizations are available for this account")
		}
		selected = resp.Organizations[0].ID
	}
	return ensureCurrentOrgKey(ctx, s, apiURL, selected)
}

// ensureCurrentOrgKey fetches or creates the device-bound managed CLI API key
// for orgID and caches it in state.
func ensureCurrentOrgKey(ctx context.Context, s *state.CliState, apiURL, orgID string) error {
	if s.SupabaseSession == nil {
		return ErrNotLoggedIn
	}
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "dari-cli"
	}
	var issued managedKeyResponse
	path := "/v1/organizations/" + orgID + "/managed-cli-key/ensure"
	client := api.New(apiURL).WithBearer(s.SupabaseSession.AccessToken)
	if err := client.Do(ctx, http.MethodPost, path, map[string]string{"device_name": hostname}, &issued); err != nil {
		return fmt.Errorf("ensure managed CLI key: %w", err)
	}
	org, ok := s.Organizations[orgID]
	if !ok {
		return fmt.Errorf("organization %q is not known locally", orgID)
	}
	org.APIKey = issued.APIKey
	s.Organizations[orgID] = org
	s.CurrentOrgID = orgID
	return nil
}

// syncOrganizations reconciles a freshly-fetched org list with the locally
// cached managed API keys. Orgs removed server-side are dropped.
func syncOrganizations(s *state.CliState, orgs []OrgRecord) {
	existingKeys := make(map[string]string, len(s.Organizations))
	for id, org := range s.Organizations {
		existingKeys[id] = org.APIKey
	}
	synced := make(map[string]state.Organization, len(orgs))
	for _, o := range orgs {
		synced[o.ID] = state.Organization{
			ID:     o.ID,
			Name:   o.Name,
			Slug:   o.Slug,
			Role:   o.Role,
			APIKey: existingKeys[o.ID],
		}
	}
	s.Organizations = synced
	if _, ok := synced[s.CurrentOrgID]; !ok {
		s.CurrentOrgID = ""
		for id := range synced {
			s.CurrentOrgID = id
			break
		}
	}
}

// ListOrganizations calls GET /v1/organizations, syncs local state, and
// persists. Returns the raw server list (useful for CLI output that includes
// fields beyond what state.Organization tracks).
func ListOrganizations(ctx context.Context, apiURL string) (*state.CliState, []OrgRecord, error) {
	if EnvAPIKeyValue() != "" {
		return nil, nil, ErrNeedsUserLogin
	}
	var resp orgsListResponse
	s, err := DoAuthenticated(ctx, apiURL, http.MethodGet, "/v1/organizations", nil, &resp)
	if err != nil {
		return nil, nil, err
	}
	syncOrganizations(s, resp.Organizations)
	if err := state.Save(s); err != nil {
		return nil, nil, err
	}
	return s, resp.Organizations, nil
}

// CreateOrganization creates a new org, caches its managed CLI key, and makes
// it the current org.
func CreateOrganization(ctx context.Context, apiURL, name string) (*state.CliState, error) {
	if EnvAPIKeyValue() != "" {
		return nil, ErrNeedsUserLogin
	}
	var created OrgRecord
	s, err := DoAuthenticated(ctx, apiURL, http.MethodPost, "/v1/organizations", map[string]string{"name": name}, &created)
	if err != nil {
		return nil, err
	}
	// Upsert manually (not a full sync — we haven't refetched the list).
	existing, ok := s.Organizations[created.ID]
	s.Organizations[created.ID] = state.Organization{
		ID:     created.ID,
		Name:   created.Name,
		Slug:   created.Slug,
		Role:   created.Role,
		APIKey: existingKey(existing, ok),
	}
	if err := ensureCurrentOrgKey(ctx, s, apiURL, created.ID); err != nil {
		return nil, err
	}
	if err := state.Save(s); err != nil {
		return nil, err
	}
	return s, nil
}

// SwitchOrganization syncs the org list, matches identifier against id or
// slug, and caches the managed key for the match.
func SwitchOrganization(ctx context.Context, apiURL, identifier string) (*state.CliState, error) {
	if EnvAPIKeyValue() != "" {
		return nil, ErrNeedsUserLogin
	}
	s, orgs, err := ListOrganizations(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	var matched *OrgRecord
	for i := range orgs {
		if orgs[i].ID == identifier || orgs[i].Slug == identifier {
			matched = &orgs[i]
			break
		}
	}
	if matched == nil {
		return nil, fmt.Errorf("unknown organization %q", identifier)
	}
	if err := ensureCurrentOrgKey(ctx, s, apiURL, matched.ID); err != nil {
		return nil, err
	}
	if err := state.Save(s); err != nil {
		return nil, err
	}
	return s, nil
}

func existingKey(org state.Organization, ok bool) string {
	if !ok {
		return ""
	}
	return org.APIKey
}
