package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/api"
)

type agentSubscriptionStatus struct {
	Credentials        []agentCredential `json:"credentials"`
	AutoroutingAllowed *bool             `json:"autorouting_allowed"`
}

// routerSelectsModels reports whether requests on the router need model
// selection: more than one model/thinking-level pair is Autorouting. A model
// without listed levels resolves to every level it supports, so it counts as
// selecting rather than as one pair.
func routerSelectsModels(router agentRouter) bool {
	pairs := 0
	for _, model := range router.EnabledModels {
		levels := len(router.ModelThinkingLevels[model])
		if levels == 0 {
			return true
		}
		pairs += levels
	}
	return pairs > 1
}

// Gateway serves routers with one model and thinking level; anything that
// selects needs the Autorouting plan. The browser handles pricing,
// organization selection and payment; the CLI only opens billing after an
// explicit choice and rechecks the plan before launching.
func ensureAgentAutoroutingPlan(ctx context.Context, client *api.Client, router agentRouter, stdin io.Reader, stderr io.Writer) error {
	if !routerSelectsModels(router) {
		return nil
	}
	var status agentSubscriptionStatus
	if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &status); err != nil {
		return api.HumanError(err)
	}
	if status.AutoroutingAllowed == nil || *status.AutoroutingAllowed {
		return nil
	}
	explanation := "This router chooses between models, which requires the Autorouting plan ($30/month for individuals)."
	for {
		var choice int
		var err error
		if agentInputIsTerminal(stdin) {
			choice, err = runAgentChoiceTUI(
				stdin, stderr, "Autorouting", "Enable Autorouting",
				[]agentChoiceOption{
					{label: "Subscribe to Autorouting"},
					{label: "Check Plan"},
					{label: "Cancel"},
				},
				agentPlanDetails,
			)
		} else {
			fmt.Fprintln(stderr, explanation)
			fmt.Fprint(stderr, "[1] Subscribe to Autorouting [2] Check my plan [3] Cancel: ")
			var answer string
			answer, err = readAgentLine(stdin)
			if err == nil {
				switch strings.TrimSpace(answer) {
				case "", "1":
					choice = 0
				case "2":
					choice = 1
				case "3":
					choice = 2
				default:
					return errors.New("expected 1, 2, or 3")
				}
			}
		}
		if err != nil {
			return fmt.Errorf("autorouting setup was not completed: %w", err)
		}
		if choice == 2 {
			return errors.New("autorouting requires the Autorouting plan; subscribe on the Usage page, or keep one model and thinking level on this router")
		}
		if choice == 0 {
			var config struct {
				WebAppURL string `json:"web_app_url"`
			}
			if err := api.New(client.BaseURL).Do(ctx, http.MethodGet, "/v1/auth/config", nil, &config); err != nil {
				return api.HumanError(err)
			}
			webURL, err := url.Parse(config.WebAppURL)
			if err != nil || webURL.Host == "" || (webURL.Scheme != "http" && webURL.Scheme != "https") {
				return errors.New("API returned an invalid web app URL for billing")
			}
			billingURL := strings.TrimRight(config.WebAppURL, "/") + "/usage"
			if openAgentBrowser(billingURL) {
				fmt.Fprintln(stderr, "Opened Autorouting checkout in your browser.")
			} else {
				fmt.Fprintln(stderr, "Open this URL to subscribe to Autorouting:")
			}
			fmt.Fprintf(stderr, "  %s\n", billingURL)
			fmt.Fprintln(stderr, "Subscribe in the browser, then return here and press Enter to check again.")
			if _, err := readAgentLine(stdin); err != nil {
				return fmt.Errorf("wait for Autorouting subscription: %w", err)
			}
		}
		if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, &status); err != nil {
			return api.HumanError(err)
		}
		if status.AutoroutingAllowed == nil || *status.AutoroutingAllowed {
			fmt.Fprintln(stderr, "Autorouting is active.")
			return nil
		}
		fmt.Fprintln(stderr, "Autorouting is not active for this organization yet. Check the organization in the browser, then try again.")
	}
}

// Keep plan copy narrow and neutral; green is reserved for successful steps.
func agentPlanDetails(width int) []string {
	paragraphs := []string{
		"Autorouting picks a model for every request: $30 per month for individuals.",
		"Gateway is free: subscriptions, managed billing, and your own keys on a router with one model.",
		"Your provider subscription is separate.",
	}
	if width <= 0 || width > 60 {
		width = 60
	}
	lines := []string{}
	for _, paragraph := range paragraphs {
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len(line)+1+len(word) > width {
				lines = append(lines, ansiGray+line+ansiReset)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		lines = append(lines, ansiGray+line+ansiReset)
	}
	return append(lines, "")
}
