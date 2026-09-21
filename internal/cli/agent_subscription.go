package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

type agentSubscriptionProvider struct {
	id, name, callbackAddress, callbackBaseURL, callbackPath string
}

var (
	claudeSubscription = agentSubscriptionProvider{"anthropic_claude_code", "Claude Code", claudeOAuthCallbackAddress, "http://localhost:53692", "/callback"}
	codexSubscription  = agentSubscriptionProvider{"openai_codex", "ChatGPT", "", "", ""}
)

// Codex and Pi use the organization's default router, but their subscription
// decision is per user and launcher, not implied by a cached router or login.
func ensureDefaultAgentSubscription(ctx context.Context, agent string, access agentRoutingAccess, client *api.Client, router agentRouter, stdin io.Reader, stderr io.Writer) error {
	scope, err := agentSubscriptionPreferenceScope(access.scope, agent)
	if err != nil {
		return err
	}
	optedOut, err := state.AgentSubscriptionOptedOut(scope)
	if err != nil {
		return err
	}
	provider := codexSubscription
	if agent == "pi" {
		// Reuse either supported subscription before asking which one to connect.
		var listed agentSubscriptionStatus
		if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &listed); err != nil {
			return api.HumanError(err)
		}
		if optedOut && listed.Allowed != nil && !*listed.Allowed {
			return nil
		}
		allowed, err := ensureAgentSubscriptionPlan(ctx, client, stdin, stderr, &listed)
		if err != nil {
			return err
		}
		if !allowed {
			return state.SaveAgentSubscriptionOptOut(scope, true)
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
		choice := 0
		if agentInputIsTerminal(stdin) {
			choice, err = runAgentChoiceTUI(stdin, stderr, "Connect a Subscription", "Which subscription do you want to connect?", []agentChoiceOption{{label: "ChatGPT"}, {label: "Claude Code"}, {label: "Use Dari Managed Billing"}}, nil)
		} else {
			fmt.Fprint(stderr, "Connect a subscription: [1] ChatGPT [2] Claude Code [3] Dari managed billing: ")
			var answer string
			answer, err = readAgentLine(stdin)
			if err == io.EOF {
				return nil
			}
			if err == nil {
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
			}
		}
		if err != nil {
			return err
		}
		if choice == 2 {
			return state.SaveAgentSubscriptionOptOut(scope, true)
		}
		if choice == 1 {
			provider = claudeSubscription
		}
		if err := connectAgentPersonalSubscription(ctx, provider, client, stdin, stderr); err != nil {
			return err
		}
		if err := state.SaveAgentSubscriptionOptOut(scope, false); err != nil {
			return err
		}
		return setAgentRouterSubscription(ctx, client, router, true, router.PersonalOAuthFallbackEnabled)
	}
	resolution, err := resolveAgentPersonalSubscription(ctx, provider, client, stdin, stderr, optedOut)
	if err != nil {
		return err
	}
	if resolution.preferenceChanged {
		if err := state.SaveAgentSubscriptionOptOut(scope, !resolution.enabled); err != nil {
			return err
		}
	}
	// Enable the shared router only when this launch newly connected a
	// subscription. Do not disable it when another user opts out, and do not
	// re-enable it just because a credential already exists.
	if resolution.connectedNow {
		return setAgentRouterSubscription(ctx, client, router, true, router.PersonalOAuthFallbackEnabled)
	}
	return nil
}
