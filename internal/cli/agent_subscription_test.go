package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func TestLoggedInAgentConnectsMissingSubscription(t *testing.T) {
	for _, tc := range []struct{ agent, provider, input string }{
		{"codex", "openai_codex", "y\n\n"},
		{"claude", "anthropic_claude_code", "y\ntest-code#test-state\n"},
		{"pi", "openai_codex", "1\n\n"},
		{"pi", "anthropic_claude_code", "2\ntest-code#test-state\n"},
		{"opencode", "openai_codex", "1\n\n"},
		{"opencode", "anthropic_claude_code", "2\ntest-code#test-state\n"},
	} {
		t.Run(tc.agent+"/"+tc.provider, func(t *testing.T) {
			t.Setenv("DARI_CONFIG_DIR", t.TempDir())
			t.Setenv("DARI_API_KEY", "")
			t.Setenv(agents.RoutingAPIKeyEnv, "")
			var connected, updated bool
			server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer dari_org" {
					t.Errorf("unexpected auth")
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/organizations/current/routers":
					_ = json.NewEncoder(w).Encode(map[string]any{"routers": []agentRouter{
						{ID: "default", Name: "Default", IsDefault: true, EnabledModels: []string{"openai/test", "anthropic/test"}, ModelProviders: map[string]string{"openai/test": "openai", "anthropic/test": "anthropic"}},
						{ID: "claude", Name: "Claude", ClientKey: claudeRouterClientKey, EnabledModels: []string{"anthropic/test"}, ModelProviders: map[string]string{"anthropic/test": "anthropic"}},
					}})
				case "GET /v1/organizations/current/credentials":
					credentials := []agentCredential{}
					if connected {
						credentials = append(credentials, agentCredential{OAuthProvider: tc.provider})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"credentials": credentials})
				case "POST /v1/organizations/current/credentials/oauth/sessions":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["provider"] != tc.provider {
						t.Errorf("provider = %s", body["provider"])
					}
					started := agentSubscriptionOAuthStart{SessionToken: "session", AuthorizationURL: "https://example.test/oauth"}
					if tc.provider == "openai_codex" {
						started.UserCode = "TEST-CODE"
					}
					_ = json.NewEncoder(w).Encode(started)
				case "POST /v1/organizations/current/credentials/oauth/sessions/complete":
					var body map[string]string
					_ = json.NewDecoder(r.Body).Decode(&body)
					validResponse := body["authorization_response"] == "test-code#test-state"
					if tc.provider == "openai_codex" {
						validResponse = body["authorization_response"] == ""
					}
					if body["session_token"] != "session" || !validResponse {
						t.Errorf("invalid completion: %#v", body)
					}
					connected = true
					w.WriteHeader(http.StatusOK)
				case "PUT /v1/organizations/current/routers/default", "PUT /v1/organizations/current/routers/claude":
					var body agentRouterSubscriptionUpdateRequest
					_ = json.NewDecoder(r.Body).Decode(&body)
					updated = slices.Contains(body.SubscriptionProviders, map[string]string{"openai_codex": "openai", "anthropic_claude_code": "anthropic"}[tc.provider])
					_ = json.NewEncoder(w).Encode(map[string]any{})
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			t.Setenv("DARI_API_URL", server.URL)
			if err := state.Save(&state.CliState{APIURL: server.URL, CurrentOrgID: "org", SupabaseSession: &state.SupabaseSession{UserID: "user"}, Organizations: map[string]state.Organization{"org": {ID: "org", APIKey: "dari_org"}}}); err != nil {
				t.Fatal(err)
			}
			scope := server.URL + "|org:org"
			if _, _, err := state.EnsureRoutingKey(scope, func() (string, error) { return "dari_route", nil }); err != nil {
				t.Fatal(err)
			}
			original := openAgentBrowser
			openAgentBrowser = func(string) bool { return false }
			t.Cleanup(func() { openAgentBrowser = original })
			var output strings.Builder
			access, err := resolveAgentRoutingAccess(context.Background(), strings.NewReader(""), &output)
			if err != nil {
				t.Fatal(err)
			}
			if access.key != "dari_route" {
				t.Fatalf("key = %s", access.key)
			}
			_, err = ensureAgentRouter(context.Background(), tc.agent, access, strings.NewReader(tc.input), &output)
			if err != nil {
				t.Fatal(err)
			}
			if !connected || !updated {
				t.Fatalf("connected=%v updated=%v output=%s", connected, updated, output.String())
			}
			if strings.Contains(output.String(), "Sign in to") {
				t.Fatal("asked already logged-in user to sign in")
			}
			output.Reset()
			_, err = ensureAgentRouter(context.Background(), tc.agent, access, strings.NewReader(""), &output)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), "subscription?") || strings.Contains(output.String(), "Connect a subscription:") {
				t.Fatalf("prompted connected user: %s", output.String())
			}
		})
	}
}

func TestDefaultAgentSubscriptionDeclineAndEOF(t *testing.T) {
	for _, agent := range []string{"codex", "opencode", "pi"} {
		for _, answer := range []string{"", "decline"} {
			t.Run(agent+"/"+answer, func(t *testing.T) {
				useTestAPIKey(t)
				server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
				}))
				defer server.Close()
				input := ""
				if answer == "decline" {
					input = "n\n"
					if agent == "opencode" || agent == "pi" {
						input = "3\n"
					}
				}
				access := agentRoutingAccess{apiURL: server.URL, scope: "test"}
				_, err := ensureDefaultAgentSubscription(context.Background(), agent, access, api.New(server.URL), agentRouter{}, strings.NewReader(input), io.Discard)
				if err != nil {
					t.Fatal(err)
				}
				optedOut, err := agentSubscriptionOptedOut(context.Background(), api.New(server.URL), agent)
				if err != nil {
					t.Fatal(err)
				}
				if optedOut != (answer == "decline") {
					t.Fatalf("opted out = %v", optedOut)
				}
				var output strings.Builder
				_, err = ensureDefaultAgentSubscription(context.Background(), agent, access, api.New(server.URL), agentRouter{}, strings.NewReader(""), &output)
				if err != nil {
					t.Fatal(err)
				}
				if answer == "decline" && output.Len() != 0 {
					t.Fatalf("prompted after opt-out: %s", output.String())
				}
			})
		}
	}
}

func TestDefaultAgentSubscriptionDoesNotEnableRouterWhenAlreadyConnected(t *testing.T) {
	for _, tc := range []struct{ agent, provider string }{
		{"codex", "openai_codex"},
		{"pi", "openai_codex"},
		{"pi", "anthropic_claude_code"},
		{"opencode", "openai_codex"},
		{"opencode", "anthropic_claude_code"},
	} {
		t.Run(tc.agent+"/"+tc.provider, func(t *testing.T) {
			useTestAPIKey(t)
			var putRequests int
			server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/organizations/current/credentials":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"credentials": []agentCredential{{OAuthProvider: tc.provider}},
					})
				default:
					if r.Method == http.MethodPut {
						putRequests++
					}
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			access := agentRoutingAccess{apiURL: server.URL, scope: "test"}
			router := agentRouter{
				ID:            "default",
				Name:          "Dari Recommended Router",
				IsDefault:     true,
				EnabledModels: []string{"openai/test"},
			}
			_, err := ensureDefaultAgentSubscription(context.Background(), tc.agent, access, api.New(server.URL), router, strings.NewReader(""), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if putRequests != 0 {
				t.Fatalf("PUT requests = %d, want 0", putRequests)
			}
		})
	}
}

func TestCodexDeviceCodeSubscriptionUsesCodeAndEmptyCompletion(t *testing.T) {
	useTestAPIKey(t)
	var completed map[string]any
	server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/organizations/current/credentials/oauth/sessions":
			_ = json.NewEncoder(w).Encode(agentSubscriptionOAuthStart{SessionToken: "session", AuthorizationURL: "https://auth.openai.com/codex/device", UserCode: "ABCD-EFGH"})
		case "POST /v1/organizations/current/credentials/oauth/sessions/complete":
			_ = json.NewDecoder(r.Body).Decode(&completed)
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	var output strings.Builder
	original := openAgentBrowser
	openAgentBrowser = func(string) bool { return false }
	t.Cleanup(func() { openAgentBrowser = original })
	err := connectAgentPersonalSubscription(context.Background(), codexSubscription, api.New(server.URL), strings.NewReader("\n"), &output, "")
	if err != nil {
		t.Fatal(err)
	}
	if completed["session_token"] != "session" || completed["authorization_response"] != "" {
		t.Fatalf("completion = %#v", completed)
	}
	for _, want := range []string{"ABCD-EFGH", "auth.openai.com/codex/device"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output.String())
		}
	}
}

func TestMuseCodeSubscriptionRetriesWhileMetaApprovalIsPending(t *testing.T) {
	useTestAPIKey(t)
	completions := 0
	server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/organizations/current/credentials/oauth/sessions":
			_ = json.NewEncoder(w).Encode(agentSubscriptionOAuthStart{SessionToken: "session", AuthorizationURL: "https://auth.meta.com/oauth/device/?code=BLXG-CHNZ", UserCode: "BLXG-CHNZ"})
		case "POST /v1/organizations/current/credentials/oauth/sessions/complete":
			completions++
			if completions == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]any{"detail": "Meta approval is still pending. Enter the code at Meta, finish approval, then try again."})
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	var output strings.Builder
	original := openAgentBrowser
	openAgentBrowser = func(string) bool { return false }
	t.Cleanup(func() { openAgentBrowser = original })
	err := connectAgentPersonalSubscription(context.Background(), museSubscription, api.New(server.URL), strings.NewReader("\n\n"), &output, "")
	if err != nil {
		t.Fatal(err)
	}
	if completions != 2 {
		t.Fatalf("completions = %d, want 2", completions)
	}
	for _, want := range []string{"Enter this code at Meta: BLXG-CHNZ", "Meta approval is still pending.", "Muse Code subscription connected"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("output missing %q: %s", want, output.String())
		}
	}
}

func TestAgentSubscriptionLinesListInRoutingOrder(t *testing.T) {
	label := "Work"
	lines := agentSubscriptionLines(personalSubscriptions([]subscriptionCredential{
		{Name: "CLAUDE_2", OAuthProvider: claudeSubscription.id, CredentialScope: "user", CreatedAt: "2026-02", Label: &label},
		{Name: "CODEX", OAuthProvider: codexSubscription.id, CredentialScope: "user", CreatedAt: "2026-01", SubscriptionActive: true, SafeMetadata: map[string]string{"account_email": "me@example.test"}},
		{Name: "CLAUDE", OAuthProvider: claudeSubscription.id, CredentialScope: "user", CreatedAt: "2026-01"},
		{Name: "TEAM", OAuthProvider: codexSubscription.id, CredentialScope: "organization"},
		{Name: "MUSE", OAuthProvider: museSubscription.id, CredentialScope: "user", CreatedAt: "2026-03"},
	}), true)
	got := stripANSI(strings.Join(lines, "\n"))
	want := "  Claude Code  CLAUDE           standby\n  Claude Code  Work             standby\n  Muse Code    MUSE             standby\n  ChatGPT      me@example.test  active\n"
	if got != want {
		t.Fatalf("lines:\n%q\nwant:\n%q", got, want)
	}
	if empty := stripANSI(strings.Join(agentSubscriptionLines(nil, false), "\n")); !strings.Contains(empty, "No accounts connected yet.") {
		t.Fatalf("empty list: %q", empty)
	}
}

func TestAgentConnectNeedsATerminal(t *testing.T) {
	err := connectAgentSubscriptionsOnRequest(context.Background(), "pi", agentRoutingAccess{}, strings.NewReader(""), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("err = %v", err)
	}
}

func TestAgentSubscriptionProvidersMatchTheAgent(t *testing.T) {
	for agent, want := range map[string][]agentSubscriptionProvider{
		"claude":   {claudeSubscription},
		"codex":    {codexSubscription},
		"opencode": {codexSubscription, claudeSubscription},
		"pi":       {codexSubscription, claudeSubscription},
	} {
		if got := agentSubscriptionProviders(agent); !slices.Equal(got, want) {
			t.Errorf("%s: providers = %v, want %v", agent, got, want)
		}
	}
}

func TestAgentSubscriptionAddsSurviveAnErrorOnTheAccountsPage(t *testing.T) {
	useTestAPIKey(t)
	var puts int
	server := newAgentTestServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		puts++
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_default"})
	}))
	defer server.Close()
	client := api.New(server.URL)
	router := agentRouter{ID: "rtr_default", Name: "Default", EnabledModels: []string{"openai/gpt-5.6-sol"}, ModelProviders: map[string]string{"openai/gpt-5.6-sol": "openai"}}
	pageErr := errors.New("agent onboarding canceled")

	// One account was connected before Esc: it is stored, so routing must use it.
	if err := recordAgentSubscriptionAdds(context.Background(), client, router, agentSubscriptionProviders("pi"), "pi", 1, pageErr); err != pageErr {
		t.Fatalf("error = %v, want the page error", err)
	}
	if puts != 1 {
		t.Fatalf("router updates = %d, want the router routed through the account", puts)
	}
	if optedOut, err := agentSubscriptionOptedOut(context.Background(), client, "pi"); err != nil || optedOut {
		t.Fatalf("opted out = %v, err = %v; want subscriptions on", optedOut, err)
	}

	// Nothing connected and the page failed: no preference is recorded.
	if err := recordAgentSubscriptionAdds(context.Background(), client, router, agentSubscriptionProviders("codex"), "codex", 0, pageErr); err != pageErr {
		t.Fatalf("error = %v, want the page error", err)
	}
	if optedOut, err := agentSubscriptionOptedOut(context.Background(), client, "codex"); err != nil || optedOut {
		t.Fatalf("opted out = %v, err = %v; want no preference recorded", optedOut, err)
	}
	if puts != 1 {
		t.Fatalf("router updates = %d, want none for a failed page with no accounts", puts)
	}
}
