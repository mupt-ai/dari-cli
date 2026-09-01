package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/state"
)

const (
	claudeRouterName                 = "Dari Claude Code"
	claudeManagedRouterName          = "Dari Claude Code (Managed)"
	claudeRouterClientKey            = "dari-cli:claude-code"
	claudeManagedRouterClientKey     = "dari-cli:claude-code-managed"
	claudeOAuthCallbackAddress       = "127.0.0.1:53692"
	claudeOAuthAutomaticWaitDuration = 30 * time.Second
)

// claudeDefaultModelIDs is the recommended Claude Code model set — distinct
// from the Sol-based org default router Codex keeps.
var claudeDefaultModelIDs = []string{
	"anthropic/claude-fable-5",
	"anthropic/claude-opus-5",
	"zai-org/GLM-5.3-Flash",
}

var (
	openAgentBrowser             = auth.OpenBrowser
	claudeOAuthAutomaticWaitTime = claudeOAuthAutomaticWaitDuration
)

var errClaudeOAuthCallbackTimedOut = errors.New("timed out waiting for Anthropic authorization")

var defaultAgentEvalIDs = []string{
	"evl_public_aa_long_context_reasoning",
	"evl_public_designarena_code_overall",
	"evl_public_aa_intelligence_index",
	"evl_public_designarena_data_viz",
	"evl_public_designarena_website",
	"evl_public_aa_hle",
	"evl_public_vals_terminal_bench_2_1",
	"evl_public_vals_vibe_code_bench",
	"evl_public_aa_gdpval",
	"evl_public_aa_scicode",
}

type agentRoutingAccess struct {
	apiURL string
	scope  string
	key    string
}

type agentRouter struct {
	ID                           string              `json:"id"`
	Name                         string              `json:"name"`
	ClientKey                    string              `json:"client_key"`
	IsDefault                    bool                `json:"is_default"`
	EnabledModels                []string            `json:"enabled_models"`
	ModelProviders               map[string]string   `json:"model_providers"`
	ModelThinkingLevels          map[string][]string `json:"model_thinking_levels"`
	PersonalOAuthEnabled         bool                `json:"personal_oauth_enabled"`
	PersonalOAuthFallbackEnabled bool                `json:"personal_oauth_fallback_enabled"`
}

type agentModel struct {
	ID                   string   `json:"id"`
	DisplayName          string   `json:"display_name"`
	Provider             string   `json:"provider"`
	DefaultProvider      string   `json:"default_provider"`
	DefaultThinkingLevel string   `json:"default_thinking_level"`
	SupportedLevels      []string `json:"supported_thinking_levels"`
	SupportsManagedKey   bool     `json:"supports_managed_key"`
}

type agentModelCatalog struct {
	Groups []struct {
		SupportsManagedKey bool         `json:"supports_managed_key"`
		Models             []agentModel `json:"models"`
	} `json:"groups"`
}

type agentModelChoice struct {
	ID     string
	Levels []string
}

type agentRouterCreateRequest struct {
	Name                              string              `json:"name"`
	ClientKey                         string              `json:"client_key"`
	EnabledModels                     []string            `json:"enabled_models"`
	ModelProviders                    map[string]string   `json:"model_providers"`
	ProviderKeySources                map[string]string   `json:"provider_key_sources"`
	EvalIDs                           []string            `json:"eval_ids"`
	ModelThinkingLevels               map[string][]string `json:"model_thinking_levels"`
	RoutingStrategy                   string              `json:"routing_strategy"`
	SpeculativeRouting                bool                `json:"speculative_routing"`
	PrimaryRetries                    int                 `json:"primary_retries"`
	ModelFallbackEnabled              bool                `json:"model_fallback_enabled"`
	FallbackRequiresDifferentProvider bool                `json:"fallback_requires_different_provider"`
	PersonalOAuthEnabled              bool                `json:"personal_oauth_enabled"`
	PersonalOAuthFallbackEnabled      bool                `json:"personal_oauth_fallback_enabled"`
	EvalScoreImputation               bool                `json:"eval_score_imputation"`
}

type agentRouterUpdateRequest struct {
	Name                string              `json:"name"`
	EnabledModels       []string            `json:"enabled_models"`
	ModelProviders      map[string]string   `json:"model_providers"`
	ProviderKeySources  map[string]string   `json:"provider_key_sources,omitempty"`
	ModelThinkingLevels map[string][]string `json:"model_thinking_levels"`
}

type claudeRouterSubscriptionUpdateRequest struct {
	Name                         string              `json:"name"`
	EnabledModels                []string            `json:"enabled_models"`
	ModelProviders               map[string]string   `json:"model_providers,omitempty"`
	ModelThinkingLevels          map[string][]string `json:"model_thinking_levels,omitempty"`
	PersonalOAuthEnabled         bool                `json:"personal_oauth_enabled"`
	PersonalOAuthFallbackEnabled bool                `json:"personal_oauth_fallback_enabled"`
}

type agentCredential struct {
	OAuthProvider string `json:"oauth_provider"`
}

type agentSubscriptionOAuthStart struct {
	SessionToken     string `json:"session_token"`
	AuthorizationURL string `json:"authorization_url"`
}

type claudeSubscriptionResolution struct {
	enabled           bool
	decided           bool
	preferenceChanged bool
}

type claudeOAuthCallbackServer struct {
	server   *http.Server
	response chan string
}

func resolveAgentRoutingAccess(
	ctx context.Context,
	stdin io.Reader,
	stderr io.Writer,
) (agentRoutingAccess, error) {
	apiURL, err := (&globalFlags{}).resolveAPIURL()
	if err != nil {
		return agentRoutingAccess{}, err
	}
	scope, err := agentKeyScope(ctx, apiURL, stdin, stderr)
	if err != nil {
		return agentRoutingAccess{}, err
	}
	key, err := resolveAgentRoutingKeyForScope(ctx, apiURL, scope, stderr)
	if err != nil {
		return agentRoutingAccess{}, err
	}
	return agentRoutingAccess{apiURL: apiURL, scope: scope, key: key}, nil
}

func ensureAgentRouter(
	ctx context.Context,
	agent string,
	access agentRoutingAccess,
	stdin io.Reader,
	stderr io.Writer,
) (string, error) {
	if agent != "claude" && agent != "codex" {
		return "default", nil
	}

	scope := access.scope + "|agent:" + agent
	cachedRouterID, err := state.AgentRouterID(scope)
	if err != nil {
		return "", err
	}
	if cachedRouterID != "" && agent != "claude" {
		return cachedRouterID, nil
	}

	client, err := auth.OrgKeyClient(access.apiURL)
	if err != nil {
		return "", err
	}
	routers, err := listAgentRouters(ctx, client)
	if err != nil {
		return "", err
	}
	defaultRouter, ok := findAgentRouter(routers, func(router agentRouter) bool {
		return router.IsDefault
	})
	if !ok {
		return "", errors.New("organization has no default router")
	}

	usePersonalSubscription := false
	claudeClientKey := ""
	if agent == "claude" {
		preferenceScope, err := claudeSubscriptionPreferenceScope(access.scope)
		if err != nil {
			return "", err
		}
		previouslyOptedOut, err := state.AgentSubscriptionOptedOut(preferenceScope)
		if err != nil {
			return "", err
		}
		resolution, err := resolveClaudePersonalSubscription(
			ctx,
			client,
			stdin,
			stderr,
			previouslyOptedOut,
		)
		if err != nil {
			return "", err
		}
		if !resolution.decided {
			if cached, found := findAgentRouter(routers, func(router agentRouter) bool {
				return router.ID == cachedRouterID && router.ClientKey == claudeManagedRouterClientKey
			}); found {
				return cached.ID, nil
			}
			if existing, found := findAgentRouter(routers, func(router agentRouter) bool {
				return router.ClientKey == claudeManagedRouterClientKey
			}); found {
				return existing.ID, nil
			}
			return defaultRouter.ID, nil
		}
		usePersonalSubscription = resolution.enabled
		if resolution.preferenceChanged {
			if err := state.SaveAgentSubscriptionOptOut(preferenceScope, !usePersonalSubscription); err != nil {
				return "", err
			}
		}
		claudeClientKey = claudeManagedRouterClientKey
		if usePersonalSubscription {
			claudeClientKey = claudeRouterClientKey
		}
		existing, found := findAgentRouter(routers, func(router agentRouter) bool {
			return router.ClientKey == claudeClientKey
		})
		if found {
			if err := setClaudeRouterSubscription(ctx, client, existing, usePersonalSubscription); err != nil {
				return "", err
			}
			if err := state.SaveAgentRouterID(scope, existing.ID); err != nil {
				return "", err
			}
			return existing.ID, nil
		}
	}

	title := "Configure the default router for Codex"
	defaultIDs := defaultRouter.EnabledModels
	currentLevels := defaultRouter.ModelThinkingLevels
	recommendedLevels := agentRouterRecommendedLevels(defaultRouter)
	if agent == "claude" {
		// Claude Code gets its own recommendation, independent of the org
		// default router Codex uses, so the two stay distinct.
		title = "Configure a router for Claude Code"
		defaultIDs = claudeDefaultModelIDs
		currentLevels = nil
		recommendedLevels = claudeRecommendedLevels()
	}
	models, err := listAgentModels(ctx, client)
	if err != nil {
		return "", err
	}
	if agent != "claude" {
		models = includeCurrentAgentModels(models, defaultRouter)
	}
	choices, err := selectAgentModels(stdin, stderr, title, models, defaultIDs, currentLevels, recommendedLevels)
	if err != nil {
		return "", err
	}

	routerID := defaultRouter.ID
	if agent == "codex" {
		if agentRouterNeedsUpdate(defaultRouter, choices) {
			body := agentRouterUpdateBody(defaultRouter, models, choices)
			path := "/v1/organizations/current/routers/" + url.PathEscape(defaultRouter.ID)
			if err := client.Do(ctx, http.MethodPut, path, body, &defaultRouter); err != nil {
				return "", api.HumanError(err)
			}
		}
		fmt.Fprintln(stderr, "Codex will use your existing default router.")
	} else {
		body := agentRouterCreateBody(models, choices, claudeClientKey, usePersonalSubscription)
		var created agentRouter
		createErr := client.Do(ctx, http.MethodPost, "/v1/organizations/current/routers", body, &created)
		if createErr == nil {
			routerID = created.ID
			fmt.Fprintln(stderr, "Created a Claude Code router.")
		} else if api.AsHTTPError(createErr) != nil && api.AsHTTPError(createErr).Status == http.StatusConflict {
			routers, err := listAgentRouters(ctx, client)
			if err != nil {
				return "", err
			}
			existing, found := findAgentRouter(routers, func(router agentRouter) bool {
				return router.ClientKey == claudeClientKey
			})
			if !found {
				return "", api.HumanError(createErr)
			}
			if err := setClaudeRouterSubscription(ctx, client, existing, usePersonalSubscription); err != nil {
				return "", err
			}
			routerID = existing.ID
		} else {
			return "", api.HumanError(createErr)
		}
	}
	if err := state.SaveAgentRouterID(scope, routerID); err != nil {
		return "", err
	}
	return routerID, nil
}

func claudeSubscriptionPreferenceScope(accessScope string) (string, error) {
	scope := accessScope + "|agent:claude"
	if !strings.Contains(accessScope, "|org:") {
		return scope, nil
	}
	current, err := state.Load()
	if err != nil {
		return "", err
	}
	if current.SupabaseSession == nil || strings.TrimSpace(current.SupabaseSession.UserID) == "" {
		return scope, nil
	}
	return scope + "|user:" + current.SupabaseSession.UserID, nil
}

func resolveClaudePersonalSubscription(
	ctx context.Context,
	client *api.Client,
	stdin io.Reader,
	stderr io.Writer,
	previouslyOptedOut bool,
) (claudeSubscriptionResolution, error) {
	var listed struct {
		Credentials []agentCredential `json:"credentials"`
	}
	if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &listed); err != nil {
		return claudeSubscriptionResolution{}, api.HumanError(err)
	}
	if slices.ContainsFunc(listed.Credentials, func(credential agentCredential) bool {
		return credential.OAuthProvider == "anthropic_claude_code"
	}) {
		return claudeSubscriptionResolution{enabled: true, decided: true, preferenceChanged: previouslyOptedOut}, nil
	}
	if previouslyOptedOut {
		return claudeSubscriptionResolution{decided: true}, nil
	}

	var useSubscription, answered bool
	if agentInputIsTerminal(stdin) {
		choice, err := runAgentChoiceTUI(
			stdin,
			stderr,
			"Connect Claude Code",
			"Use your Claude Code personal subscription?",
			[]agentChoiceOption{
				{label: "Use Claude Code subscription", note: "recommended"},
				{label: "Use Dari managed billing"},
			},
			nil,
		)
		if err != nil {
			return claudeSubscriptionResolution{}, err
		}
		useSubscription = choice == 0
		answered = true
	} else {
		var err error
		useSubscription, answered, err = confirmDefaultYes(
			stdin,
			stderr,
			"Use your Claude Code personal subscription? [Y/n] ",
		)
		if err != nil {
			return claudeSubscriptionResolution{}, err
		}
	}
	if !answered {
		return claudeSubscriptionResolution{}, nil
	}
	if !useSubscription {
		return claudeSubscriptionResolution{decided: true, preferenceChanged: true}, nil
	}
	if err := connectClaudePersonalSubscription(ctx, client, stdin, stderr); err != nil {
		return claudeSubscriptionResolution{}, err
	}
	return claudeSubscriptionResolution{enabled: true, decided: true, preferenceChanged: true}, nil
}

func connectClaudePersonalSubscription(
	ctx context.Context,
	client *api.Client,
	stdin io.Reader,
	stderr io.Writer,
) error {
	var started agentSubscriptionOAuthStart
	if err := client.Do(
		ctx,
		http.MethodPost,
		"/v1/organizations/current/credentials/oauth/sessions",
		map[string]string{"provider": "anthropic_claude_code"},
		&started,
	); err != nil {
		return api.HumanError(err)
	}
	if strings.TrimSpace(started.SessionToken) == "" || strings.TrimSpace(started.AuthorizationURL) == "" {
		return errors.New("Dari returned an incomplete Claude Code authorization session")
	}

	callback, callbackErr := startClaudeOAuthCallbackServer()
	if callback != nil {
		defer callback.Close()
	}
	styled := agentInputIsTerminal(stdin)
	opened := openAgentBrowser(started.AuthorizationURL)
	if styled {
		authorizationResponse, err := runClaudeOAuthTUI(
			stdin,
			stderr,
			started.AuthorizationURL,
			opened,
			callback,
		)
		if err != nil {
			return err
		}
		return completeClaudePersonalSubscription(ctx, client, stderr, started.SessionToken, authorizationResponse, true)
	}

	writeClaudeOAuthInstructions(stderr, started.AuthorizationURL, opened)
	var (
		authorizationResponse string
		err                   error
	)
	if opened && callbackErr == nil {
		if response, received := callback.Try(); received {
			authorizationResponse = response
		} else {
			fmt.Fprint(stderr, "Paste localhost callback URL, or press Enter to wait automatically: ")
			manualResponse, readErr := readAgentLine(stdin)
			if readErr != nil && !errors.Is(readErr, io.EOF) {
				return fmt.Errorf("read Claude Code callback URL: %w", readErr)
			}
			if strings.TrimSpace(manualResponse) != "" {
				callback.Close()
				authorizationResponse = manualResponse
			} else {
				fmt.Fprintln(stderr, "Waiting for Anthropic authorization…")
				authorizationResponse, err = callback.Wait(ctx, claudeOAuthAutomaticWaitTime)
				if errors.Is(err, errClaudeOAuthCallbackTimedOut) {
					callback.Close()
					fmt.Fprintln(stderr, "Automatic callback was not received. Copy the full localhost URL from your browser; the error page is expected.")
					fmt.Fprint(stderr, "Paste localhost URL: ")
					authorizationResponse, err = readAgentLine(stdin)
				}
			}
		}
	} else {
		fmt.Fprintln(stderr, "After approving, copy the full localhost URL from your browser. The error page is expected.")
		fmt.Fprint(stderr, "Paste localhost URL: ")
		authorizationResponse, err = readAgentLine(stdin)
	}
	if err != nil {
		return fmt.Errorf("complete Claude Code authorization: %w", err)
	}
	return completeClaudePersonalSubscription(ctx, client, stderr, started.SessionToken, authorizationResponse, false)
}

func completeClaudePersonalSubscription(
	ctx context.Context,
	client *api.Client,
	stderr io.Writer,
	sessionToken string,
	authorizationResponse string,
	styled bool,
) error {
	if strings.TrimSpace(authorizationResponse) == "" {
		return errors.New("Claude Code authorization was not completed")
	}
	if err := client.Do(
		ctx,
		http.MethodPost,
		"/v1/organizations/current/credentials/oauth/sessions/complete",
		map[string]string{
			"session_token":          sessionToken,
			"authorization_response": authorizationResponse,
		},
		nil,
	); err != nil {
		return api.HumanError(err)
	}
	if styled {
		fmt.Fprintln(stderr, ansiGreen+"✓ Claude Code personal subscription connected."+ansiReset)
	} else {
		fmt.Fprintln(stderr, "Connected your Claude Code personal subscription.")
	}
	return nil
}

func writeClaudeOAuthInstructions(stderr io.Writer, authorizationURL string, opened bool) {
	if opened {
		fmt.Fprintf(stderr, "Opened Anthropic login in your browser. If it did not open, visit:\n  %s\n", authorizationURL)
	} else {
		fmt.Fprintf(stderr, "Open this URL to connect your Claude Code subscription:\n  %s\n", authorizationURL)
	}
}

func startClaudeOAuthCallbackServer() (*claudeOAuthCallbackServer, error) {
	listener, err := net.Listen("tcp", claudeOAuthCallbackAddress)
	if err != nil {
		return nil, err
	}
	callback := &claudeOAuthCallbackServer{
		response: make(chan string, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, request *http.Request) {
		select {
		case callback.response <- request.URL.String():
		default:
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Claude Code subscription connected. You can close this tab.\n")
	})
	callback.server = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = callback.server.Serve(listener) }()
	return callback, nil
}

func (callback *claudeOAuthCallbackServer) Try() (string, bool) {
	select {
	case response := <-callback.response:
		return "http://localhost:53692" + response, true
	default:
		return "", false
	}
}

func (callback *claudeOAuthCallbackServer) Wait(ctx context.Context, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case response := <-callback.response:
		return "http://localhost:53692" + response, nil
	case <-timer.C:
		return "", errClaudeOAuthCallbackTimedOut
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (callback *claudeOAuthCallbackServer) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = callback.server.Shutdown(ctx)
}

func confirmDefaultYes(stdin io.Reader, stderr io.Writer, prompt string) (bool, bool, error) {
	fmt.Fprint(stderr, prompt)
	answer, err := readAgentLine(stdin)
	if errors.Is(err, io.EOF) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true, true, nil
	case "n", "no":
		return false, true, nil
	default:
		return false, true, fmt.Errorf("expected yes or no, got %q", strings.TrimSpace(answer))
	}
}

func readAgentLine(stdin io.Reader) (string, error) {
	var line strings.Builder
	for {
		var chunk [1]byte
		n, readErr := stdin.Read(chunk[:])
		if n > 0 {
			if chunk[0] == '\n' {
				return line.String(), nil
			}
			line.WriteByte(chunk[0])
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) && line.Len() > 0 {
				return line.String(), nil
			}
			return "", readErr
		}
	}
}

func setClaudeRouterSubscription(
	ctx context.Context,
	client *api.Client,
	router agentRouter,
	enabled bool,
) error {
	if router.PersonalOAuthEnabled == enabled && !router.PersonalOAuthFallbackEnabled {
		return nil
	}
	if strings.TrimSpace(router.Name) == "" || len(router.EnabledModels) == 0 {
		return errors.New("Dari returned an incomplete Claude Code router")
	}
	body := claudeRouterSubscriptionUpdateRequest{
		Name:                         router.Name,
		EnabledModels:                router.EnabledModels,
		ModelProviders:               router.ModelProviders,
		ModelThinkingLevels:          router.ModelThinkingLevels,
		PersonalOAuthEnabled:         enabled,
		PersonalOAuthFallbackEnabled: false,
	}
	path := "/v1/organizations/current/routers/" + url.PathEscape(router.ID)
	if err := client.Do(ctx, http.MethodPut, path, body, &router); err != nil {
		return api.HumanError(err)
	}
	return nil
}

func listAgentRouters(ctx context.Context, client *api.Client) ([]agentRouter, error) {
	var listed struct {
		Routers []agentRouter `json:"routers"`
	}
	if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/routers", nil, &listed); err != nil {
		return nil, api.HumanError(err)
	}
	return listed.Routers, nil
}

func findAgentRouter(
	routers []agentRouter,
	match func(agentRouter) bool,
) (agentRouter, bool) {
	for _, router := range routers {
		if match(router) {
			return router, true
		}
	}
	return agentRouter{}, false
}

func listAgentModels(ctx context.Context, client *api.Client) ([]agentModel, error) {
	var catalog agentModelCatalog
	if err := client.Do(
		ctx,
		http.MethodGet,
		"/v1/organizations/current/routers/model-catalog",
		nil,
		&catalog,
	); err != nil {
		return nil, api.HumanError(err)
	}
	models := []agentModel{}
	for _, group := range catalog.Groups {
		if !group.SupportsManagedKey {
			continue
		}
		for _, model := range group.Models {
			if !model.SupportsManagedKey {
				continue
			}
			models = append(models, model)
		}
	}
	sort.SliceStable(models, func(i, j int) bool {
		return strings.ToLower(models[i].DisplayName) < strings.ToLower(models[j].DisplayName)
	})
	return models, nil
}

func includeCurrentAgentModels(models []agentModel, current agentRouter) []agentModel {
	known := make(map[string]bool, len(models))
	for _, model := range models {
		known[model.ID] = true
	}
	for _, modelID := range current.EnabledModels {
		if known[modelID] {
			continue
		}
		models = append(models, agentModel{
			ID:              modelID,
			DisplayName:     modelID,
			Provider:        current.ModelProviders[modelID],
			DefaultProvider: current.ModelProviders[modelID],
		})
	}
	return models
}

func selectAgentModels(
	stdin io.Reader,
	stderr io.Writer,
	title string,
	models []agentModel,
	defaults []string,
	currentLevels map[string][]string,
	recommendedLevels map[string][]string,
) ([]agentModelChoice, error) {
	if agentInputIsTerminal(stdin) {
		return runAgentPicker(stdin, stderr, title, models, defaults, currentLevels, recommendedLevels)
	}
	return selectAgentModelsByNumber(stdin, stderr, title, models, defaults, currentLevels, recommendedLevels)
}

func selectAgentModelsByNumber(
	stdin io.Reader,
	stderr io.Writer,
	title string,
	models []agentModel,
	defaults []string,
	currentLevels map[string][]string,
	recommendedLevels map[string][]string,
) ([]agentModelChoice, error) {
	byID := make(map[string]agentModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}

	fmt.Fprintf(stderr, "\n%s\n\n", title)
	for i, model := range models {
		mark := " "
		if slices.Contains(defaults, model.ID) {
			mark = "x"
		}
		fmt.Fprintf(stderr, "  %2d. [%s] %s\n", i+1, mark, model.DisplayName)
	}
	fmt.Fprintln(stderr, "\nEnter model numbers separated by commas, or press Enter to keep the checked models.")
	fmt.Fprint(stderr, "> ")

	// Read via readAgentLine so unconsumed stdin still reaches the agent
	// process launched after setup.
	line, readErr := readAgentLine(stdin)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return nil, fmt.Errorf("read model selection: %w", readErr)
	}
	input := strings.TrimSpace(line)
	if input == "" {
		if len(defaults) == 0 {
			return nil, errors.New("at least one model must be selected")
		}
		choices := make([]agentModelChoice, 0, len(defaults))
		for _, id := range defaults {
			choices = append(choices, agentModelChoice{
				ID:     id,
				Levels: defaultLevelsFor(byID[id], currentLevels[id], recommendedLevels[id]),
			})
		}
		return choices, nil
	}

	chosen := []agentModelChoice{}
	for _, raw := range strings.Split(input, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || index < 1 || index > len(models) {
			return nil, fmt.Errorf("invalid model number %q", strings.TrimSpace(raw))
		}
		model := models[index-1]
		if !slices.ContainsFunc(chosen, func(choice agentModelChoice) bool { return choice.ID == model.ID }) {
			chosen = append(chosen, agentModelChoice{
				ID:     model.ID,
				Levels: defaultLevelsFor(model, currentLevels[model.ID], recommendedLevels[model.ID]),
			})
		}
	}
	if len(chosen) == 0 {
		return nil, errors.New("at least one model must be selected")
	}
	return chosen, nil
}

func defaultLevelsFor(model agentModel, current, recommended []string) []string {
	if len(current) > 0 {
		return append([]string(nil), current...)
	}
	if len(recommended) > 0 {
		return append([]string(nil), recommended...)
	}
	if levels := recommendedAgentThinkingLevels(model.ID); len(levels) > 0 {
		return levels
	}
	if model.DefaultThinkingLevel != "" {
		return []string{model.DefaultThinkingLevel}
	}
	if len(model.SupportedLevels) > 0 {
		return []string{model.SupportedLevels[0]}
	}
	return nil
}

type agentPickerRow struct {
	model       agentModel
	levelOpts   []string
	recommended map[string]bool
	checked     map[string]bool
	selected    bool
}

func (row *agentPickerRow) checkedLevels() []string {
	levels := []string{}
	for _, level := range row.levelOpts {
		if row.checked[level] {
			levels = append(levels, level)
		}
	}
	return levels
}

func (row *agentPickerRow) recommendedLevels() []string {
	levels := make([]string, 0, len(row.recommended))
	for _, level := range row.levelOpts {
		if row.recommended[level] {
			levels = append(levels, level)
		}
	}
	return levels
}

func (row *agentPickerRow) usesRecommended() bool {
	if len(row.recommended) == 0 {
		return false
	}
	levels := row.checkedLevels()
	if len(levels) != len(row.recommended) {
		return false
	}
	for _, level := range levels {
		if !row.recommended[level] {
			return false
		}
	}
	return true
}

type agentPickerState struct {
	rows        []agentPickerRow
	cursor      int
	editing     bool
	levelCursor int
}

func newAgentPickerState(
	models []agentModel,
	defaults []string,
	currentLevels map[string][]string,
	recommendedLevels map[string][]string,
) *agentPickerState {
	state := &agentPickerState{rows: make([]agentPickerRow, 0, len(models))}
	for _, model := range models {
		row := newAgentPickerRow(model, currentLevels[model.ID], recommendedLevels[model.ID])
		row.selected = slices.Contains(defaults, model.ID)
		state.rows = append(state.rows, row)
	}
	return state
}

func newAgentPickerRow(model agentModel, current, recommended []string) agentPickerRow {
	row := agentPickerRow{
		model:       model,
		recommended: make(map[string]bool, len(recommended)),
		checked:     map[string]bool{},
	}
	for _, level := range recommended {
		row.recommended[level] = true
	}
	initial := defaultLevelsFor(model, current, recommended)
	for _, levels := range [][]string{model.SupportedLevels, initial, recommended} {
		for _, level := range levels {
			if level != "" && !slices.Contains(row.levelOpts, level) {
				row.levelOpts = append(row.levelOpts, level)
			}
		}
	}
	for _, level := range initial {
		row.checked[level] = true
	}
	return row
}

func (p *agentPickerState) selectedCount() int {
	count := 0
	for _, row := range p.rows {
		if row.selected {
			count++
		}
	}
	return count
}

func (p *agentPickerState) choices() []agentModelChoice {
	choices := make([]agentModelChoice, 0, p.selectedCount())
	for _, row := range p.rows {
		if row.selected {
			choices = append(choices, agentModelChoice{
				ID:     row.model.ID,
				Levels: row.checkedLevels(),
			})
		}
	}
	return choices
}

func (p *agentPickerState) handleKey(key string) (done, canceled bool) {
	if p.editing {
		return p.handleLevelKey(key)
	}
	row := &p.rows[p.cursor]
	switch key {
	case "up", "k":
		p.cursor = (p.cursor - 1 + len(p.rows)) % len(p.rows)
	case "down", "j":
		p.cursor = (p.cursor + 1) % len(p.rows)
	case " ":
		row.selected = !row.selected
	case "right", "l":
		if len(row.levelOpts) > 0 {
			p.editing = true
			p.levelCursor = firstCheckedIndex(row)
		}
	case "enter":
		if p.selectedCount() == 0 {
			return false, false
		}
		return true, false
	case "quit":
		return false, true
	}
	return false, false
}

func (p *agentPickerState) handleLevelKey(key string) (done, canceled bool) {
	row := &p.rows[p.cursor]
	switch key {
	case "up", "k":
		p.levelCursor = (p.levelCursor - 1 + len(row.levelOpts)) % len(row.levelOpts)
	case "down", "j":
		p.levelCursor = (p.levelCursor + 1) % len(row.levelOpts)
	case " ":
		level := row.levelOpts[p.levelCursor]
		row.checked[level] = !row.checked[level]
	case "left", "h", "esc", "enter":
		p.exitEditing()
	case "quit":
		return false, true
	}
	return false, false
}

func firstCheckedIndex(row *agentPickerRow) int {
	for i, level := range row.levelOpts {
		if row.checked[level] {
			return i
		}
	}
	return 0
}

func (p *agentPickerState) exitEditing() {
	row := &p.rows[p.cursor]
	if len(row.checkedLevels()) == 0 {
		row.checked = map[string]bool{}
		for _, level := range defaultLevelsFor(row.model, nil, row.recommendedLevels()) {
			row.checked[level] = true
		}
	}
	p.editing = false
}

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiCoral = "\033[38;5;209m"
	ansiCyan  = "\033[38;5;117m"
	ansiGreen = "\033[38;5;114m"
	ansiGray  = "\033[38;5;245m"
)

const dariBanner = "DARI ROUTER"

func agentInputIsTerminal(stdin io.Reader) bool {
	file, ok := stdin.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// agentBannerLines draws the boxed "DARI ROUTER" header with the task
// title centered beneath it.
func agentBannerLines(title string) []string {
	inner := len(dariBanner)
	if len(title) > inner {
		inner = len(title)
	}
	center := func(text string) string {
		pad := inner - len(text)
		return strings.Repeat(" ", pad/2) + text + strings.Repeat(" ", pad-pad/2)
	}
	border := strings.Repeat("─", inner+2)
	return []string{
		ansiGray + "╭" + border + "╮" + ansiReset,
		ansiGray + "│ " + ansiReset + ansiCoral + ansiBold + center(dariBanner) + ansiReset + ansiGray + " │" + ansiReset,
		ansiGray + "│ " + ansiReset + center(title) + ansiGray + " │" + ansiReset,
		ansiGray + "╰" + border + "╯" + ansiReset,
		"",
	}
}

func renderAgentPicker(stderr io.Writer, title string, p *agentPickerState, termWidth int) {
	renderAgentScreen(stderr, agentPickerLines(title, p), termWidth)
}

func renderAgentScreen(stderr io.Writer, lines []string, termWidth int) {
	banner := agentBannerLines("")
	bannerPad := 0
	if termWidth > 0 && len(lines) > 0 {
		if w := ansiVisibleWidth(lines[0]); termWidth > w {
			bannerPad = (termWidth - w) / 2
		}
	}
	bannerLineCount := len(banner)
	var frame strings.Builder
	// Raw mode disables output post-processing, so newlines must be \r\n or
	// each line renders one column further right than the last.
	frame.WriteString("\033[H\033[K\r\n")
	for i, line := range lines {
		if i < bannerLineCount && bannerPad > 0 {
			frame.WriteString(strings.Repeat(" ", bannerPad))
		}
		frame.WriteString(line)
		frame.WriteString("\033[K\r\n")
	}
	frame.WriteString("\033[J")
	_, _ = stderr.Write([]byte(frame.String()))
}

// ansiVisibleWidth counts printable cells, skipping ANSI escape sequences.
func ansiVisibleWidth(s string) int {
	width, inEscape := 0, false
	for _, r := range s {
		switch {
		case inEscape:
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
		case r == '\033':
			inEscape = true
		default:
			width++
		}
	}
	return width
}

func agentPickerLines(title string, p *agentPickerState) []string {
	lines := agentBannerLines(title)
	nameWidth := 0
	for _, row := range p.rows {
		if w := len(row.model.DisplayName); w > nameWidth {
			nameWidth = w
		}
	}
	for i, row := range p.rows {
		cursor := "  "
		if i == p.cursor {
			cursor = ansiCoral + "> " + ansiReset
		}
		box := ansiGray + "[ ]" + ansiReset
		if row.selected {
			box = ansiCoral + "[x]" + ansiReset
		}
		name := fmt.Sprintf("%-*s", nameWidth, row.model.DisplayName)
		switch {
		case i == p.cursor && row.selected:
			name = ansiBold + name + ansiReset
		case i == p.cursor:
			name = ansiDim + ansiBold + name + ansiReset
		case !row.selected:
			name = ansiDim + name + ansiReset
		}
		levels := strings.Join(row.checkedLevels(), "+")
		if levels == "" {
			levels = "default"
		}
		note := ""
		if row.usesRecommended() {
			note = "  " + ansiGreen + "· recommended" + ansiReset
		}
		lines = append(lines, cursor+box+" "+name+"  "+
			ansiGray+"thinking:"+ansiReset+" "+ansiCyan+levels+ansiReset+note)
		if p.editing && i == p.cursor {
			levelWidth := 0
			for _, level := range row.levelOpts {
				if len(level) > levelWidth {
					levelWidth = len(level)
				}
			}
			for j, level := range row.levelOpts {
				levelCursor := "      "
				if j == p.levelCursor {
					levelCursor = ansiCoral + "    > " + ansiReset
				}
				levelBox := ansiGray + "[ ]" + ansiReset
				levelStyle := ansiDim
				if row.checked[level] {
					levelBox = ansiCoral + "[x]" + ansiReset
					levelStyle = ansiCyan
				}
				rec := ""
				if row.recommended[level] {
					rec = "  " + ansiGreen + "· recommended" + ansiReset
				}
				lines = append(lines, levelCursor+levelBox+" "+
					levelStyle+ansiBold+fmt.Sprintf("%-*s", levelWidth, level)+ansiReset+rec)
			}
		}
	}
	if p.editing {
		lines = append(lines, "", ansiGray+"↑/↓ move   space toggle level   ←/enter done   q cancel"+ansiReset)
	} else {
		lines = append(lines,
			"",
			ansiGray+"↑/↓ move   space toggle   → edit thinking levels   enter confirm   q cancel"+ansiReset,
			ansiGreen+"Defaults are recommended — press Enter to keep them."+ansiReset,
		)
	}
	return lines
}

func parseAgentPickerKeys(raw []byte) []string {
	keys := []string{}
	for i := 0; i < len(raw); {
		if raw[i] == 0x1b && i+2 < len(raw) && raw[i+1] == '[' {
			switch raw[i+2] {
			case 'A':
				keys = append(keys, "up")
			case 'B':
				keys = append(keys, "down")
			case 'C':
				keys = append(keys, "right")
			case 'D':
				keys = append(keys, "left")
			}
			i += 3
			continue
		}
		switch raw[i] {
		case '\r', '\n':
			keys = append(keys, "enter")
		case 0x1b:
			keys = append(keys, "esc")
		case ' ':
			keys = append(keys, " ")
		case 'q', 'Q', 0x03:
			keys = append(keys, "quit")
		case 'k':
			keys = append(keys, "k")
		case 'j':
			keys = append(keys, "j")
		case 'h':
			keys = append(keys, "h")
		case 'l':
			keys = append(keys, "l")
		}
		i++
	}
	return keys
}

func runAgentPicker(
	stdin io.Reader,
	stderr io.Writer,
	title string,
	models []agentModel,
	defaults []string,
	currentLevels map[string][]string,
	recommendedLevels map[string][]string,
) ([]agentModelChoice, error) {
	p := newAgentPickerState(models, defaults, currentLevels, recommendedLevels)
	if len(p.rows) == 0 {
		return nil, errors.New("no managed-key models are available")
	}
	session, err := startAgentTUISession(stdin, stderr)
	if err != nil {
		return nil, err
	}
	defer session.Close()

	buf := make([]byte, 16)
	for {
		renderAgentPicker(stderr, title, p, session.width)
		n, readErr := session.file.Read(buf)
		for _, key := range parseAgentPickerKeys(buf[:n]) {
			done, canceled := p.handleKey(key)
			if canceled {
				return nil, errors.New("model selection canceled")
			}
			if done {
				return p.choices(), nil
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil, errors.New("model selection canceled")
			}
			return nil, fmt.Errorf("read selection: %w", readErr)
		}
	}
}

func choiceIDs(choices []agentModelChoice) []string {
	ids := make([]string, len(choices))
	for i, choice := range choices {
		ids[i] = choice.ID
	}
	return ids
}

func choiceLevels(choices []agentModelChoice) map[string][]string {
	levels := make(map[string][]string, len(choices))
	for _, choice := range choices {
		levels[choice.ID] = append([]string(nil), choice.Levels...)
	}
	return levels
}

func agentProvidersFor(models []agentModel, ids []string) map[string]string {
	byID := make(map[string]agentModel, len(models))
	for _, model := range models {
		byID[model.ID] = model
	}
	providers := make(map[string]string, len(ids))
	for _, id := range ids {
		provider := byID[id].DefaultProvider
		if provider == "" {
			provider = byID[id].Provider
		}
		providers[id] = provider
	}
	return providers
}

func agentRouterNeedsUpdate(current agentRouter, choices []agentModelChoice) bool {
	if !sameStringSet(choiceIDs(choices), current.EnabledModels) {
		return true
	}
	for _, choice := range choices {
		if !sameStringSet(choice.Levels, current.ModelThinkingLevels[choice.ID]) {
			return true
		}
	}
	return false
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, value := range a {
		counts[value]++
	}
	for _, value := range b {
		if counts[value] == 0 {
			return false
		}
		counts[value]--
	}
	return true
}

func agentRouterCreateBody(
	models []agentModel,
	choices []agentModelChoice,
	clientKey string,
	usePersonalSubscription bool,
) agentRouterCreateRequest {
	ids := choiceIDs(choices)
	providers := agentProvidersFor(models, ids)
	providerSources := map[string]string{}
	for _, provider := range providers {
		providerSources[provider] = "managed"
	}
	name := claudeRouterName
	if clientKey == claudeManagedRouterClientKey {
		name = claudeManagedRouterName
	}
	return agentRouterCreateRequest{
		Name:                              name,
		ClientKey:                         clientKey,
		EnabledModels:                     ids,
		ModelProviders:                    providers,
		ProviderKeySources:                providerSources,
		EvalIDs:                           defaultAgentEvalIDs,
		ModelThinkingLevels:               choiceLevels(choices),
		RoutingStrategy:                   "slm",
		SpeculativeRouting:                true,
		PrimaryRetries:                    3,
		ModelFallbackEnabled:              true,
		FallbackRequiresDifferentProvider: true,
		PersonalOAuthEnabled:              usePersonalSubscription,
		PersonalOAuthFallbackEnabled:      false,
		EvalScoreImputation:               true,
	}
}

func agentRouterUpdateBody(
	current agentRouter,
	models []agentModel,
	choices []agentModelChoice,
) agentRouterUpdateRequest {
	ids := choiceIDs(choices)
	providers := agentProvidersFor(models, ids)
	currentProviders := map[string]bool{}
	for model, provider := range current.ModelProviders {
		currentProviders[provider] = true
		if slices.Contains(ids, model) {
			providers[model] = provider
		}
	}
	providerSources := map[string]string{}
	for _, provider := range providers {
		if !currentProviders[provider] {
			providerSources[provider] = "managed"
		}
	}
	return agentRouterUpdateRequest{
		Name:                current.Name,
		EnabledModels:       ids,
		ModelProviders:      providers,
		ProviderKeySources:  providerSources,
		ModelThinkingLevels: choiceLevels(choices),
	}
}

func claudeRecommendedLevels() map[string][]string {
	levels := make(map[string][]string, len(claudeDefaultModelIDs))
	for _, modelID := range claudeDefaultModelIDs {
		levels[modelID] = recommendedAgentThinkingLevels(modelID)
	}
	return levels
}

func agentRouterRecommendedLevels(router agentRouter) map[string][]string {
	levels := make(map[string][]string, len(router.EnabledModels))
	for _, modelID := range router.EnabledModels {
		if configured := router.ModelThinkingLevels[modelID]; len(configured) > 0 {
			levels[modelID] = append([]string(nil), configured...)
		}
	}
	return levels
}

func recommendedAgentThinkingLevels(modelID string) []string {
	switch {
	case modelID == "openai/gpt-5.6-sol":
		return []string{"medium", "xhigh"}
	case modelID == "anthropic/claude-fable-5", modelID == "anthropic/claude-opus-5":
		return []string{"high"}
	case strings.HasPrefix(modelID, "zai-org/GLM-5.3"):
		return []string{"high"}
	default:
		return nil
	}
}
