package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/api"
	"github.com/mupt-ai/dari-cli/internal/state"
)

func TestLoggedInAgentConnectsMissingSubscription(t *testing.T) {
	for _, tc := range []struct{ agent, provider, input string }{
		{"codex", "openai_codex", "y\n\n"},
		{"claude", "anthropic_claude_code", "y\nhttp://localhost:53692/callback?code=test&state=test\n"},
		{"pi", "openai_codex", "1\n\n"},
		{"pi", "anthropic_claude_code", "2\nhttp://localhost:53692/callback?code=test&state=test\n"},
	} {
		t.Run(tc.agent+"/"+tc.provider, func(t *testing.T) {
			t.Setenv("DARI_CONFIG_DIR", t.TempDir())
			t.Setenv("DARI_API_KEY", "")
			t.Setenv(routingAPIKeyEnv, "")
			var connected, updated bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer dari_org" {
					t.Errorf("unexpected auth")
				}
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/organizations/current/routers":
					_ = json.NewEncoder(w).Encode(map[string]any{"routers": []agentRouter{
						{ID: "default", Name: "Default", IsDefault: true, EnabledModels: []string{"openai/test"}},
						{ID: "claude", Name: "Claude", ClientKey: claudeRouterClientKey, EnabledModels: []string{"anthropic/test"}},
					}})
				case "GET /v1/organizations/current/credentials":
					credentials := []agentCredential{}
					if connected {
						credentials = append(credentials, agentCredential{OAuthProvider: tc.provider})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"credentials": credentials, "personal_subscriptions_allowed": true})
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
					validResponse := strings.Contains(body["authorization_response"], "code=test")
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
					updated = body.PersonalOAuthEnabled
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
			if err := state.SaveAgentRouterID(scope+"|agent:"+tc.agent, "cached"); err != nil {
				t.Fatal(err)
			}
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
	for _, agent := range []string{"codex", "pi"} {
		for _, answer := range []string{"", "decline"} {
			t.Run(agent+"/"+answer, func(t *testing.T) {
				useTestAPIKey(t)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet || r.URL.Path != "/v1/organizations/current/credentials" {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
						http.NotFound(w, r)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}, "personal_subscriptions_allowed": true})
				}))
				defer server.Close()
				input := ""
				if answer == "decline" {
					input = "n\n"
					if agent == "pi" {
						input = "3\n"
					}
				}
				access := agentRoutingAccess{apiURL: server.URL, scope: "test"}
				err := ensureDefaultAgentSubscription(context.Background(), agent, access, api.New(server.URL), agentRouter{}, strings.NewReader(input), io.Discard)
				if err != nil {
					t.Fatal(err)
				}
				optedOut, err := state.AgentSubscriptionOptedOut("test|agent:" + agent)
				if err != nil {
					t.Fatal(err)
				}
				if optedOut != (answer == "decline") {
					t.Fatalf("opted out = %v", optedOut)
				}
				var output strings.Builder
				err = ensureDefaultAgentSubscription(context.Background(), agent, access, api.New(server.URL), agentRouter{}, strings.NewReader(""), &output)
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
	} {
		t.Run(tc.agent+"/"+tc.provider, func(t *testing.T) {
			useTestAPIKey(t)
			var putRequests int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /v1/organizations/current/credentials":
					_ = json.NewEncoder(w).Encode(map[string]any{
						"credentials":                    []agentCredential{{OAuthProvider: tc.provider}},
						"personal_subscriptions_allowed": true,
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
			err := ensureDefaultAgentSubscription(context.Background(), tc.agent, access, api.New(server.URL), router, strings.NewReader(""), io.Discard)
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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	err := connectAgentPersonalSubscription(context.Background(), codexSubscription, api.New(server.URL), strings.NewReader("\n"), &output)
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
