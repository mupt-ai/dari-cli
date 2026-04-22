// Package auth implements Dari's browser login, session refresh, and
// authenticated API request flow. It owns the Supabase gotrue PKCE dance and
// the local OAuth callback server; the HTTP client itself lives in
// internal/api.
package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

const (
	loginTimeout          = 5 * time.Minute
	sessionRefreshLeeway  = 60 * time.Second

	// EnvAPIKey is a bearer token that bypasses `dari auth login` entirely.
	// When set, all CLI commands authenticate with it in place of the cached
	// Supabase JWT / managed org key.
	EnvAPIKey = "DARI_API_KEY"

	// EnvOrgID names the organization for commands that build paths like
	// /v1/organizations/{id}/... while running headless with EnvAPIKey.
	EnvOrgID = "DARI_ORG_ID"
)

// Errors returned by this package are intentionally simple: callers print
// err.Error() verbatim to stderr. Use errors.Is to check auth-specific cases.
var (
	ErrNotLoggedIn = errors.New("no CLI login is available for this API URL. Run `dari auth login` first, or set DARI_API_KEY")
	ErrNoCurrentOrg = errors.New("no current organization is selected. Run `dari org switch <org>`, or set DARI_ORG_ID")
	ErrNeedsUserLogin = errors.New("this command manages user/org membership and requires `dari auth login`; DARI_API_KEY cannot be used here")
)

// EnvAPIKeyValue returns the DARI_API_KEY env var, trimmed. Empty means unset.
func EnvAPIKeyValue() string {
	return strings.TrimSpace(os.Getenv(EnvAPIKey))
}

// EnvOrgIDValue returns the DARI_ORG_ID env var, trimmed. Empty means unset.
func EnvOrgIDValue() string {
	return strings.TrimSpace(os.Getenv(EnvOrgID))
}

// Status is what `dari auth status` prints.
type Status struct {
	APIURL      string              `json:"api_url"`
	Email       string              `json:"email"`
	CurrentOrg  *state.Organization `json:"current_org"`
	LoggedIn    bool                `json:"logged_in"`
	SessionMode string              `json:"session_mode"`
}

// Login runs the full browser login flow: PKCE + callback + Supabase token
// exchange + /v1/me/bootstrap + /v1/organizations/{id}/managed-cli-key/ensure.
// The resulting state is persisted to disk.
//
// Login is a no-op when DARI_API_KEY is set: headless auth does not need
// cached state.
func Login(ctx context.Context, apiURL string) (*state.CliState, error) {
	if EnvAPIKeyValue() != "" {
		return nil, errors.New("DARI_API_KEY is set; `dari auth login` is not needed (unset the env var to use a browser login)")
	}
	apiURL = api.NormalizeURL(apiURL)

	cfg, err := fetchAuthConfig(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetch CLI auth configuration: %w", err)
	}
	sb := newSupabaseClient(cfg)

	pkce, err := newPKCEPair()
	if err != nil {
		return nil, err
	}
	server, err := startCallbackServer()
	if err != nil {
		return nil, err
	}
	defer server.Close()

	authorizeURL := sb.buildAuthorizeURL("google", server.RedirectURL, pkce.Challenge)
	if !openBrowser(authorizeURL) {
		fmt.Fprintf(os.Stderr, "Open this URL in a browser to continue login:\n  %s\n", authorizeURL)
	}

	cb, err := server.Wait(ctx, loginTimeout)
	if err != nil {
		return nil, err
	}
	if cb.Error != "" {
		return nil, fmt.Errorf("browser login failed: %s", cb.Error)
	}

	sess, err := sb.exchangeCode(ctx, cb.Code, pkce.Verifier)
	if err != nil {
		return nil, fmt.Errorf("exchange auth code: %w", err)
	}

	s := &state.CliState{
		APIURL:          apiURL,
		SupabaseSession: sessionToStored(sess),
		Organizations:   map[string]state.Organization{},
	}

	if err := bootstrapAndSelectOrg(ctx, s, apiURL, ""); err != nil {
		return nil, err
	}
	if err := state.Save(s); err != nil {
		return nil, err
	}
	return s, nil
}

// Logout best-effort revokes the Supabase session and clears local state.
func Logout(ctx context.Context, apiURL string) error {
	apiURL = api.NormalizeURL(apiURL)
	s, err := state.Load()
	if err != nil {
		return err
	}
	if !api.URLsMatch(s.APIURL, apiURL) || s.SupabaseSession == nil {
		// Nothing local to clear for this API URL.
		return nil
	}
	cfg, err := fetchAuthConfig(ctx, apiURL)
	if err == nil {
		_ = newSupabaseClient(cfg).signOut(ctx, s.SupabaseSession.AccessToken)
	}
	return state.Save(&state.CliState{Organizations: map[string]state.Organization{}})
}

// CurrentStatus returns the cached auth status without making network calls.
func CurrentStatus(apiURL string) (Status, error) {
	apiURL = api.NormalizeURL(apiURL)
	if EnvAPIKeyValue() != "" {
		status := Status{
			APIURL:      api.NormalizeURL(apiURL),
			LoggedIn:    true,
			SessionMode: "env_key",
		}
		if id := EnvOrgIDValue(); id != "" {
			status.CurrentOrg = &state.Organization{ID: id}
		}
		return status, nil
	}
	s, err := state.Load()
	if err != nil {
		return Status{}, err
	}
	if !api.URLsMatch(s.APIURL, apiURL) {
		return Status{}, nil
	}
	org := s.CurrentOrg()

	hasFreshJWT := s.SupabaseSession != nil && !storedSessionNeedsRefresh(s.SupabaseSession)
	hasCachedKey := org != nil && org.APIKey != ""

	mode := ""
	switch {
	case hasFreshJWT:
		mode = "jwt"
	case hasCachedKey:
		mode = "api_key"
	}
	if mode == "" {
		return Status{}, nil
	}
	email := ""
	if s.SupabaseSession != nil {
		email = s.SupabaseSession.Email
	}
	return Status{
		APIURL:      s.APIURL,
		Email:       email,
		CurrentOrg:  org,
		LoggedIn:    true,
		SessionMode: mode,
	}, nil
}

// DoAuthenticated issues an API request using the cached JWT, refreshing it
// on demand (proactively if expired, or reactively on a 401/403). The
// resulting CliState (possibly mutated with a refreshed session) is saved
// back to disk before returning.
//
// If DARI_API_KEY is set it takes precedence: the key is used as the bearer,
// the JWT refresh dance is skipped entirely, and no state is written.
func DoAuthenticated(ctx context.Context, apiURL, method, path string, body, out any) (*state.CliState, error) {
	apiURL = api.NormalizeURL(apiURL)
	if key := EnvAPIKeyValue(); key != "" {
		err := api.New(apiURL).WithBearer(key).Do(ctx, method, path, body, out)
		return nil, translateAuthError(err)
	}
	s, err := state.Load()
	if err != nil {
		return nil, err
	}
	if !api.URLsMatch(s.APIURL, apiURL) || s.SupabaseSession == nil {
		return nil, ErrNotLoggedIn
	}

	usedCached := true
	if storedSessionNeedsRefresh(s.SupabaseSession) {
		if err := refresh(ctx, s, apiURL); err != nil {
			return nil, err
		}
		if err := state.Save(s); err != nil {
			return nil, err
		}
		usedCached = false
	}

	client := api.New(apiURL).WithBearer(s.SupabaseSession.AccessToken)
	err = client.Do(ctx, method, path, body, out)
	if err == nil {
		return s, nil
	}
	if he := api.AsHTTPError(err); he != nil && usedCached && (he.Status == 401 || he.Status == 403) {
		if rerr := refresh(ctx, s, apiURL); rerr != nil {
			return nil, rerr
		}
		if err := state.Save(s); err != nil {
			return nil, err
		}
		client = api.New(apiURL).WithBearer(s.SupabaseSession.AccessToken)
		err = client.Do(ctx, method, path, body, out)
	}
	return s, translateAuthError(err)
}

// refresh exchanges the stored refresh_token for a new session and mutates s
// in place. It does not persist; the caller is responsible for saving.
func refresh(ctx context.Context, s *state.CliState, apiURL string) error {
	if s.SupabaseSession == nil {
		return ErrNotLoggedIn
	}
	cfg, err := fetchAuthConfig(ctx, apiURL)
	if err != nil {
		return fmt.Errorf("fetch CLI auth configuration: %w", err)
	}
	sess, err := newSupabaseClient(cfg).refresh(ctx, s.SupabaseSession.RefreshToken)
	if err != nil {
		return fmt.Errorf("refresh supabase session: %w", err)
	}
	s.APIURL = apiURL
	s.SupabaseSession = sessionToStored(sess)
	return nil
}

func storedSessionNeedsRefresh(s *state.SupabaseSession) bool {
	if s == nil || s.ExpiresAt == nil {
		return true
	}
	return *s.ExpiresAt <= time.Now().Unix()+int64(sessionRefreshLeeway.Seconds())
}

func sessionToStored(sess *supabaseSession) *state.SupabaseSession {
	expires := sess.ExpiresAt
	return &state.SupabaseSession{
		AccessToken:  sess.AccessToken,
		RefreshToken: sess.RefreshToken,
		ExpiresAt:    &expires,
		UserID:       sess.User.ID,
		Email:        sess.User.Email,
		DisplayName:  displayNameFromUser(sess.User),
	}
}

func translateAuthError(err error) error {
	if err == nil {
		return nil
	}
	if he := api.AsHTTPError(err); he != nil {
		if he.Status == 401 || he.Status == 403 {
			return fmt.Errorf("%w: %s", ErrNotLoggedIn, he.Detail)
		}
		return errors.New(strings.TrimSpace(he.Detail))
	}
	return err
}

// rawBearer performs a single request with the given JWT. Used during
// the login flow where state isn't yet persisted.
func rawBearer(ctx context.Context, apiURL, token, method, path string, body, out any) error {
	return api.New(apiURL).WithBearer(token).Do(ctx, method, path, body, out)
}

// OrgKeyClient returns an api.Client authenticated with the cached managed
// CLI API key (the `dari_...` bearer) for the current organization. Used by
// data-plane commands (agent, session, file) that the server authenticates
// via the org API key rather than the user JWT.
//
// If DARI_API_KEY is set it takes precedence, skipping the state/login cache.
func OrgKeyClient(apiURL string) (*api.Client, error) {
	apiURL = api.NormalizeURL(apiURL)
	if key := EnvAPIKeyValue(); key != "" {
		return api.New(apiURL).WithBearer(key), nil
	}
	s, err := state.Load()
	if err != nil {
		return nil, err
	}
	if !api.URLsMatch(s.APIURL, apiURL) {
		return nil, ErrNotLoggedIn
	}
	org := s.CurrentOrg()
	if org == nil {
		return nil, ErrNoCurrentOrg
	}
	if org.APIKey == "" {
		return nil, fmt.Errorf("no cached API key for current org; run `dari org switch %s` to refresh", org.Slug)
	}
	return api.New(apiURL).WithBearer(org.APIKey), nil
}
