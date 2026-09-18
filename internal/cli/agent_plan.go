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
	Credentials []agentCredential `json:"credentials"`
	Allowed     *bool             `json:"personal_subscriptions_allowed"`
}

// The browser handles pricing, organization selection and payment confirmation.
// The CLI only opens billing after an explicit choice and verifies entitlement
// before proceeding to provider authorization.
func ensureAgentSubscriptionPlan(ctx context.Context, client *api.Client, stdin io.Reader, stderr io.Writer, status *agentSubscriptionStatus) (bool, error) {
	if status.Allowed == nil || *status.Allowed {
		return true, nil
	}
	explanation := "Using your ChatGPT or Claude subscription through Dari requires a Dari Pro plan for this organization. Your provider subscription is separate."
	for {
		var choice int
		var err error
		if agentInputIsTerminal(stdin) {
			choice, err = runAgentChoiceTUI(
				stdin, stderr, "Subscription Setup", "Enable Dari Pro",
				[]agentChoiceOption{
					{label: "Subscribe to Dari Pro"},
					{label: "Check Subscription"},
					{label: "Use Managed Billing"},
				},
				agentPlanDetails,
			)
		} else {
			fmt.Fprintln(stderr, explanation)
			fmt.Fprint(stderr, "[1] Subscribe to Dari Pro [2] Check my subscription [3] Use Dari managed billing: ")
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
					return false, errors.New("expected 1, 2, or 3")
				}
			}
		}
		if err != nil {
			return false, fmt.Errorf("subscription onboarding was not completed: %w", err)
		}
		if choice == 2 {
			return false, nil
		}
		if choice == 0 {
			var config struct {
				WebAppURL string `json:"web_app_url"`
			}
			if err := api.New(client.BaseURL).Do(ctx, http.MethodGet, "/v1/auth/config", nil, &config); err != nil {
				return false, api.HumanError(err)
			}
			webURL, err := url.Parse(config.WebAppURL)
			if err != nil || webURL.Host == "" || (webURL.Scheme != "http" && webURL.Scheme != "https") {
				return false, errors.New("API returned an invalid web app URL for billing")
			}
			billingURL := strings.TrimRight(config.WebAppURL, "/") + "/usage"
			if openAgentBrowser(billingURL) {
				fmt.Fprintln(stderr, "Opened Dari Pro subscription in your browser.")
			} else {
				fmt.Fprintln(stderr, "Open this URL to subscribe to Dari Pro:")
			}
			fmt.Fprintf(stderr, "  %s\n", billingURL)
			fmt.Fprintln(stderr, "Subscribe in the browser, then return here and press Enter to check again.")
			if _, err := readAgentLine(stdin); err != nil {
				return false, fmt.Errorf("wait for Dari Pro subscription: %w", err)
			}
		}
		if err := client.Do(ctx, http.MethodGet, "/v1/organizations/current/credentials", nil, status); err != nil {
			return false, api.HumanError(err)
		}
		if status.Allowed != nil && *status.Allowed {
			fmt.Fprintln(stderr, "Dari Pro is active. Next: connect your provider subscription.")
			return true, nil
		}
		fmt.Fprintln(stderr, "Dari Pro is not active for this organization yet. Check the organization in the browser, then try again.")
	}
}

// Keep plan copy narrow and neutral; green is reserved for successful steps.
func agentPlanDetails(width int) []string {
	paragraphs := []string{
		"Dari Pro lets you use your ChatGPT or Claude subscription.",
		"It is separate from your provider plan.",
		"Alternatively, pay for usage with Dari managed billing.",
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
