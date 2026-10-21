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

var selectingRouter = agentRouter{
	ID:            "rtr_select",
	EnabledModels: []string{"openai/gpt-5.6-sol", "anthropic/claude-opus-5"},
}

func TestRouterSelectsModelsCountsModelThinkingPairs(t *testing.T) {
	for _, tc := range []struct {
		router agentRouter
		want   bool
	}{
		{agentRouter{EnabledModels: []string{"openai/gpt-5.6-sol"}}, true},
		{agentRouter{EnabledModels: []string{"openai/gpt-5.6-sol"}, ModelThinkingLevels: map[string][]string{"openai/gpt-5.6-sol": {}}}, true},
		{agentRouter{EnabledModels: []string{"openai/gpt-5.6-sol"}, ModelThinkingLevels: map[string][]string{"openai/gpt-5.6-sol": {"high"}}}, false},
		{agentRouter{EnabledModels: []string{"openai/gpt-5.6-sol"}, ModelThinkingLevels: map[string][]string{"openai/gpt-5.6-sol": {"low", "high"}}}, true},
		{selectingRouter, true},
	} {
		if got := routerSelectsModels(tc.router); got != tc.want {
			t.Errorf("routerSelectsModels(%+v) = %v, want %v", tc.router, got, tc.want)
		}
	}
}

func TestAgentAutoroutingPlanSkipsGatewayRoutersAndPlanHolders(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_ = json.NewEncoder(w).Encode(map[string]any{"autorouting_allowed": true})
	}))
	defer server.Close()
	gateway := agentRouter{
		EnabledModels:       []string{"openai/gpt-5.6-sol"},
		ModelThinkingLevels: map[string][]string{"openai/gpt-5.6-sol": {"high"}},
	}
	if err := ensureAgentAutoroutingPlan(context.Background(), api.New(server.URL), gateway, strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	if requests != 0 {
		t.Fatalf("gateway router checked the plan %d times", requests)
	}
	if err := ensureAgentAutoroutingPlan(context.Background(), api.New(server.URL), selectingRouter, strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentAutoroutingPlanSubscribeExplainsBrowserStepAndWaits(t *testing.T) {
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
			_ = json.NewEncoder(w).Encode(map[string]any{"autorouting_allowed": checks >= 2})
		default:
			t.Fatalf("unexpected request %s", r.URL.Path)
		}
	}))
	defer server.Close()

	var output strings.Builder
	err := ensureAgentAutoroutingPlan(context.Background(), api.New(server.URL), selectingRouter, strings.NewReader("1\n\n"), &output)
	if err != nil || checks != 2 {
		t.Fatalf("error=%v checks=%d output=%s", err, checks, output.String())
	}
	if opened != "https://app.example.test/usage" {
		t.Fatalf("opened URL = %q", opened)
	}
	for _, want := range []string{
		"Opened Autorouting checkout in your browser.",
		"Subscribe in the browser, then return here and press Enter to check again.",
		"Autorouting is active.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestAgentAutoroutingPlanCancelAndEOFFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"autorouting_allowed": false})
	}))
	defer server.Close()
	for _, input := range []string{"3\n", ""} {
		err := ensureAgentAutoroutingPlan(context.Background(), api.New(server.URL), selectingRouter, strings.NewReader(input), &strings.Builder{})
		if err == nil {
			t.Fatalf("input %q: expected an error", input)
		}
	}
}

func TestAgentAutoroutingPlanRechecksUntilActive(t *testing.T) {
	checks := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks++
		_ = json.NewEncoder(w).Encode(map[string]any{"autorouting_allowed": checks == 3})
	}))
	defer server.Close()
	var output strings.Builder
	err := ensureAgentAutoroutingPlan(context.Background(), api.New(server.URL), selectingRouter, strings.NewReader("2\n2\n"), &output)
	if err != nil || checks != 3 {
		t.Fatalf("error=%v checks=%d", err, checks)
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
