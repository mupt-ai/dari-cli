package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/api"
)

func TestAgentPlanUpgradeThenConnect(t *testing.T) {
	for _, provider := range []agentSubscriptionProvider{codexSubscription, claudeSubscription} {
		t.Run(provider.id, func(t *testing.T) {
			active := false
			original := openAgentBrowser
			defer func() { openAgentBrowser = original }()
			opened := ""
			openAgentBrowser = func(url string) bool {
				opened = url
				active = true
				return true
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/organizations/current/credentials":
					_ = json.NewEncoder(w).Encode(map[string]any{"personal_subscriptions_allowed": active, "credentials": []agentCredential{{OAuthProvider: provider.id}}})
				case "/v1/auth/config":
					_ = json.NewEncoder(w).Encode(map[string]any{"web_app_url": "https://app.example.test"})
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			var output strings.Builder
			result, err := resolveAgentPersonalSubscription(context.Background(), provider, api.New(server.URL), strings.NewReader("1\n\n"), &output, false)
			if err != nil {
				t.Fatal(err)
			}
			if !result.enabled || opened != "https://app.example.test/usage" {
				t.Fatalf("result=%+v opened=%s", result, opened)
			}
			if !strings.Contains(output.String(), "Dari Pro is active") {
				t.Fatal(output.String())
			}
		})
	}
}

func TestAgentPlanSubscribeExplainsBrowserStepAndWaits(t *testing.T) {
	checks := 0
	opened := ""
	original := openAgentBrowser
	openAgentBrowser = func(url string) bool {
		opened = url
		return true
	}
	t.Cleanup(func() { openAgentBrowser = original })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/config":
			_ = json.NewEncoder(w).Encode(map[string]any{"web_app_url": "https://app.example.test"})
		case "/v1/organizations/current/credentials":
			checks++
			_ = json.NewEncoder(w).Encode(map[string]any{"personal_subscriptions_allowed": checks >= 1})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var output strings.Builder
	allowed := false
	status := agentSubscriptionStatus{Allowed: &allowed}
	ok, err := ensureAgentSubscriptionPlan(
		context.Background(),
		api.New(server.URL),
		strings.NewReader("1\n\n"),
		&output,
		&status,
	)
	if err != nil || !ok || checks != 1 {
		t.Fatalf("allowed=%v error=%v output=%s", ok, err, output.String())
	}
	if opened != "https://app.example.test/usage" {
		t.Fatalf("opened URL = %q", opened)
	}
	for _, want := range []string{
		"Opened Dari Pro subscription in your browser.",
		"Subscribe in the browser, then return here and press Enter to check again.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestAgentPlanEOFDoesNotSilentlyUseManagedBilling(t *testing.T) {
	allowed := false
	status := agentSubscriptionStatus{Allowed: &allowed}
	var output strings.Builder
	ok, err := ensureAgentSubscriptionPlan(context.Background(), api.New("https://unused.example.test"), strings.NewReader(""), &output, &status)
	if err == nil || ok {
		t.Fatalf("allowed=%v error=%v", ok, err)
	}
}

func TestAgentPlanRechecksUntilEntitlementIsActive(t *testing.T) {
	checks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks++
		_ = json.NewEncoder(w).Encode(map[string]any{"personal_subscriptions_allowed": checks == 2})
	}))
	defer server.Close()
	allowed := false
	status := agentSubscriptionStatus{Allowed: &allowed}
	var output strings.Builder
	ok, err := ensureAgentSubscriptionPlan(context.Background(), api.New(server.URL), strings.NewReader("2\n2\n"), &output, &status)
	if err != nil || !ok || checks != 2 {
		t.Fatalf("allowed=%v error=%v checks=%d", ok, err, checks)
	}
	if !strings.Contains(output.String(), "not active") {
		t.Fatal(output.String())
	}
}

func TestAgentPlanCopyFitsCompactTerminals(t *testing.T) {
	for _, width := range []int{40, 60, 80, 160} {
		lines := agentPlanDetails(width)
		for _, line := range lines {
			if ansiVisibleWidth(line) > min(width, 60) {
				t.Fatalf("width %d: %q", width, line)
			}
			if strings.Contains(line, ansiGreen) {
				t.Fatal("plan explanation should not use success coloring")
			}
		}
	}
}
