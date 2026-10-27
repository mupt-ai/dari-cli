package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/auth"
	"github.com/mupt-ai/dari-cli/internal/state"
)

// routerProvider is the router provider whose models the subscription serves,
// and vendor the company whose sign-in connects it. A device-code sign-in shows
// a code to enter at the vendor; the others paste back the vendor's code.
type agentSubscriptionProvider struct {
	id, name, routerProvider, vendor string
	deviceCode                       bool
}

var (
	claudeSubscription = agentSubscriptionProvider{"anthropic_claude_code", "Claude Code", "anthropic", "Anthropic", false}
	codexSubscription  = agentSubscriptionProvider{"openai_codex", "ChatGPT", "openai", "OpenAI", true}
	// Muse Code subscriptions serve only Muse Code, so no agent launcher offers one.
	museSubscription = agentSubscriptionProvider{"meta_muse", "Muse Code", "meta", "Meta", true}
)

// Codex, OpenCode, Pi, and Muse use the organization's default router, but
// their subscription decision is per user and launcher, not implied by a
// cached router or login. It reports whether the Muse plan is enabled for this
// launch, so the router serves it once the model picker adds Meta models.
func ensureDefaultAgentSubscription(ctx context.Context, agent string, access agentRoutingAccess, client *api.Client, router agentRouter, stdin io.Reader, stderr io.Writer) (bool, error) {
	scope, err := agentSubscriptionPreferenceScope(access.scope, agent)
	if err != nil {
		return false, err
	}
	optedOut, err := state.AgentSubscriptionOptedOut(scope)
	if err != nil {
		return false, err
	}
	if agent == "muse" {
		// Only Muse Code can use a Muse subscription, so the launch asks for it.
		resolution, resolveErr := resolveAgentPersonalSubscription(ctx, museSubscription, client, stdin, stderr, optedOut)
		if resolution.preferenceChanged {
			if err := state.SaveAgentSubscriptionOptOut(scope, !resolution.enabled); err != nil {
				return false, err
			}
		}
		// Muse Code is launched only to use its plan, so an enabled plan is
		// served whether it was just connected or connected earlier.
		if resolution.enabled {
			if err := setAgentRouterSubscription(ctx, client, router, agentSubscriptionProviders(agent), true); err != nil {
				return false, err
			}
		}
		return resolution.enabled, resolveErr
	}
	err = ensureCodexAgentSubscription(ctx, agent, scope, optedOut, client, router, stdin, stderr)
	return false, err
}

func ensureCodexAgentSubscription(ctx context.Context, agent, scope string, optedOut bool, client *api.Client, router agentRouter, stdin io.Reader, stderr io.Writer) error {
	provider := codexSubscription
	if agent == "opencode" || agent == "pi" {
		// Reuse either supported subscription before asking which one to connect.
		var listed agentSubscriptionStatus
		if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &listed); err != nil {
			return api.HumanError(err)
		}
		connected := slices.ContainsFunc(listed.Credentials, func(c agentCredential) bool {
			return c.OAuthProvider == codexSubscription.id || c.OAuthProvider == claudeSubscription.id
		})
		// A stored credential is not a request to turn the shared default router
		// back on. Later launches must leave the router's subscription toggle alone.
		if connected {
			return nil
		}
		if optedOut {
			return nil
		}
		if agentInputIsTerminal(stdin) {
			providers := agentSubscriptionProviders(agent)
			added, manageErr := manageAgentSubscriptions(ctx, client, providers, stdin, stderr)
			return recordAgentSubscriptionAdds(ctx, client, router, providers, scope, added, manageErr)
		}
		choice := 0
		fmt.Fprint(stderr, "Connect a subscription: [1] ChatGPT [2] Claude Code [3] Dari managed billing: ")
		answer, err := readAgentLine(stdin)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch strings.TrimSpace(answer) {
		case "", "1":
			choice = 0
		case "2":
			choice = 1
		case "3":
			choice = 2
		default:
			return fmt.Errorf("expected 1, 2, or 3")
		}
		if choice == 2 {
			return state.SaveAgentSubscriptionOptOut(scope, true)
		}
		if choice == 1 {
			provider = claudeSubscription
		}
		if err := connectAgentPersonalSubscription(ctx, provider, client, stdin, stderr, ""); err != nil {
			return err
		}
		if err := state.SaveAgentSubscriptionOptOut(scope, false); err != nil {
			return err
		}
		return setAgentRouterSubscription(ctx, client, router, agentSubscriptionProviders(agent), true)
	}
	resolution, resolveErr := resolveAgentPersonalSubscription(ctx, provider, client, stdin, stderr, optedOut)
	if resolution.preferenceChanged {
		if err := state.SaveAgentSubscriptionOptOut(scope, !resolution.enabled); err != nil {
			return err
		}
	}
	// Enable the shared router only when this launch newly connected a
	// subscription. Do not disable it when another user opts out, and do not
	// re-enable it just because a credential already exists. Accounts connected
	// before an error are stored, so route through them before reporting it.
	if resolution.connectedNow {
		if err := setAgentRouterSubscription(ctx, client, router, agentSubscriptionProviders(agent), true); err != nil {
			return err
		}
	}
	return resolveErr
}

// connectAgentSubscriptionsOnRequest handles `dari <agent> --connect`: it
// opens the subscription list, and once one is added, routes this agent
// through subscriptions again even if it was set to managed billing.
func connectAgentSubscriptionsOnRequest(ctx context.Context, agent string, access agentRoutingAccess, stdin io.Reader, stderr io.Writer) error {
	if !agentInputIsTerminal(stdin) {
		return errors.New("--connect needs an interactive terminal; use `dari credentials subscription connect` instead")
	}
	client, err := auth.OrgKeyClient(access.apiURL)
	if err != nil {
		return err
	}
	added, manageErr := manageAgentSubscriptions(ctx, client, agentSubscriptionProviders(agent), stdin, stderr)
	if added == 0 {
		return manageErr
	}
	scope, err := agentSubscriptionPreferenceScope(access.scope, agent)
	if err != nil {
		return err
	}
	if err := state.SaveAgentSubscriptionOptOut(scope, false); err != nil {
		return err
	}
	// Claude Code picks its subscription router during setup; the others share
	// the default router.
	if agent == "claude" {
		return manageErr
	}
	routers, err := listAgentRouters(ctx, client)
	if err != nil {
		return err
	}
	router, ok := findAgentRouter(routers, func(router agentRouter) bool { return router.IsDefault })
	if !ok {
		return errors.New("organization has no default router")
	}
	if err := setAgentRouterSubscription(ctx, client, router, agentSubscriptionProviders(agent), true); err != nil {
		return err
	}
	return manageErr
}

// agentSubscriptionProviders are the subscriptions an agent can use: Claude
// Code only its own, Codex only ChatGPT, and OpenCode and Pi either.
func agentSubscriptionProviders(agent string) []agentSubscriptionProvider {
	switch agent {
	case "claude":
		return []agentSubscriptionProvider{claudeSubscription}
	case "codex":
		return []agentSubscriptionProvider{codexSubscription}
	case "muse":
		return []agentSubscriptionProvider{museSubscription}
	}
	return []agentSubscriptionProvider{codexSubscription, claudeSubscription}
}

// recordAgentSubscriptionAdds makes the agent's routing match the accounts
// manageAgentSubscriptions connected, even when it then ended with an error.
// Connected accounts stay stored, and later launches skip setup, so the router
// must be routed through them now. With none connected, managed billing is
// recorded unless the page failed.
func recordAgentSubscriptionAdds(ctx context.Context, client *api.Client, router agentRouter, subscriptions []agentSubscriptionProvider, scope string, added int, manageErr error) error {
	if added == 0 {
		if manageErr != nil {
			return manageErr
		}
		return state.SaveAgentSubscriptionOptOut(scope, true)
	}
	if err := state.SaveAgentSubscriptionOptOut(scope, false); err != nil {
		return err
	}
	if err := setAgentRouterSubscription(ctx, client, router, subscriptions, true); err != nil {
		return err
	}
	return manageErr
}

// manageAgentSubscriptions is the Accounts page: the user's subscriptions
// from providers, with options to connect more. With none connected it offers
// Dari managed billing instead of continuing. It reports how many it
// connected.
func manageAgentSubscriptions(ctx context.Context, client *api.Client, providers []agentSubscriptionProvider, stdin io.Reader, stderr io.Writer) (int, error) {
	added := 0
	for {
		var listed struct {
			Credentials []subscriptionCredential `json:"credentials"`
		}
		if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &listed); err != nil {
			return added, api.HumanError(err)
		}
		subscriptions := slices.DeleteFunc(personalSubscriptions(listed.Credentials), func(c subscriptionCredential) bool {
			return !slices.ContainsFunc(providers, func(p agentSubscriptionProvider) bool { return p.id == c.OAuthProvider })
		})
		routing, err := autoroutingAllowed(ctx, client)
		if err != nil {
			return added, err
		}
		// Each option is a connect for one provider, Get Autorouting, or the
		// option that leaves the page.
		options := []agentChoiceOption{}
		connects := map[int]agentSubscriptionProvider{}
		finish := agentChoiceOption{label: "Use Dari managed billing", done: "Using Dari managed billing"}
		if len(subscriptions) > 0 {
			finish = agentChoiceOption{label: "Continue", done: fmt.Sprintf("Accounts: %d connected", len(subscriptions))}
			options = append(options, finish)
		}
		for _, provider := range providers {
			label := "Connect a " + provider.name + " account"
			if len(subscriptions) > 0 {
				label = "Connect another " + provider.name + " account"
			}
			connects[len(options)] = provider
			options = append(options, agentChoiceOption{label: label})
		}
		autorouting := -1
		if !routing {
			autorouting = len(options)
			options = append(options, agentChoiceOption{label: "Get auto-routing"})
		}
		if len(subscriptions) == 0 {
			options = append(options, finish)
		}
		choice, err := runAgentChoiceTUI(stdin, stderr, "Accounts", agentSubscriptionLines(subscriptions, len(providers) > 1), options)
		if err != nil {
			return added, err
		}
		if choice == autorouting {
			if _, err := getAutorouting(ctx, client, stdin, stderr); err != nil {
				return added, err
			}
			continue
		}
		provider, connect := connects[choice]
		if !connect {
			return added, nil
		}
		if err := connectAgentPersonalSubscription(ctx, provider, client, stdin, stderr, ""); err != nil {
			return added, err
		}
		added++
	}
}

// agentSubscriptionLines lists subscriptions in the order routing uses them,
// naming each one's provider when the page offers more than one.
func agentSubscriptionLines(subscriptions []subscriptionCredential, showProvider bool) []string {
	if len(subscriptions) == 0 {
		return []string{"  " + ansiGray + "No accounts connected yet." + ansiReset, ""}
	}
	names := make([]string, len(subscriptions))
	width := 0
	for i, subscription := range subscriptions {
		names[i] = subscription.Name
		if subscription.Label != nil && strings.TrimSpace(*subscription.Label) != "" {
			names[i] = *subscription.Label
		} else if email := subscription.SafeMetadata["account_email"]; email != "" {
			names[i] = email
		}
		width = max(width, len(names[i]))
	}
	lines := []string{}
	for i, subscription := range subscriptions {
		line := "  "
		if showProvider {
			line += fmt.Sprintf("%-11s  ", subscriptionProviderFor(subscription.OAuthProvider).name)
		}
		lines = append(lines, line+fmt.Sprintf("%-*s  %s", width, names[i], ansiGray+subscriptionStatus(subscription)+ansiReset))
	}
	return append(lines, "")
}

// subscriptionProviderFor names the provider of a stored subscription.
func subscriptionProviderFor(oauthProvider string) agentSubscriptionProvider {
	for _, provider := range subscriptionProviders {
		if provider.id == oauthProvider {
			return provider
		}
	}
	return agentSubscriptionProvider{id: oauthProvider, name: oauthProvider}
}
