package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterCommandsUseOrganizationRoutes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer jwt_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/organizations/org_123/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []any{map[string]any{"id": "rtr_123"}}})
		case "/v1/organizations/org_123/routers/rtr_123":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123", "name": "Production"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	saveTestUserLogin(t, srv.URL)

	for _, args := range [][]string{
		{"--api-url", srv.URL, "router", "list"},
		{"--api-url", srv.URL, "router", "get", "https://routing.dari.dev/rtr_123/chat/completions"},
	} {
		cmd := newRootCmd("dev")
		cmd.SetArgs(args)
		if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
			t.Fatalf("dari %v: %v", args, err)
		}
	}

	for _, want := range []string{
		"GET /v1/organizations/org_123/routers",
		"GET /v1/organizations/org_123/routers/rtr_123",
	} {
		if !seen[want] {
			t.Fatalf("did not see request %s", want)
		}
	}
}

func TestEvalCommandsUseOrganizationRoutes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer jwt_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/organizations/org_123/evals":
			_ = json.NewEncoder(w).Encode(map[string]any{"evals": []any{map[string]any{"id": "eval_123"}}})
		case "/v1/organizations/org_123/evals/eval_123":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "eval_123", "name": "Code quality"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	saveTestUserLogin(t, srv.URL)

	for _, args := range [][]string{
		{"--api-url", srv.URL, "eval", "list"},
		{"--api-url", srv.URL, "eval", "get", "eval_123"},
	} {
		cmd := newRootCmd("dev")
		cmd.SetArgs(args)
		if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
			t.Fatalf("dari %v: %v", args, err)
		}
	}

	for _, want := range []string{
		"GET /v1/organizations/org_123/evals",
		"GET /v1/organizations/org_123/evals/eval_123",
	} {
		if !seen[want] {
			t.Fatalf("did not see request %s", want)
		}
	}
}
