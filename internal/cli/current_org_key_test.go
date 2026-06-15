package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestCurrentOrgCommandsUseAPIKeyRoutes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/organizations/current/credentials":
			_ = json.NewEncoder(w).Encode(map[string]any{"credentials": []any{}})
		case "PUT /v1/organizations/current/credentials/OPENAI_API_KEY":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "OPENAI_API_KEY"})
		case "DELETE /v1/organizations/current/credentials/OPENAI_API_KEY":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "OPENAI_API_KEY", "deleted_at": "2026-06-13T00:00:00Z"})
		case "GET /v1/organizations/current/members":
			_ = json.NewEncoder(w).Encode(map[string]any{"members": []any{}})
		case "POST /v1/organizations/current/invitations":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "inv_123"})
		case "PUT /v1/organizations/current/credentials/GCS_STORAGE_TEAM_KEY":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "GCS_STORAGE_TEAM_KEY"})
		case "GET /v1/organizations/current/storage-bindings":
			_ = json.NewEncoder(w).Encode(map[string]any{"storage_bindings": []map[string]any{}})
		case "POST /v1/organizations/current/storage-bindings":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "stb_123", "name": "team"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

	keyFile := t.TempDir() + "/gcs.json"
	if err := os.WriteFile(keyFile, []byte(`{"type":"service_account"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	commands := [][]string{
		{"--api-url", srv.URL, "credentials", "list"},
		{"--api-url", srv.URL, "credentials", "add", "OPENAI_API_KEY", "sk-test"},
		{"--api-url", srv.URL, "credentials", "remove", "OPENAI_API_KEY"},
		{"--api-url", srv.URL, "org", "members"},
		{"--api-url", srv.URL, "org", "invite", "new@example.test", "--role", "member"},
		{
			"--api-url", srv.URL, "storage", "connect", "gcs", "team",
			"--bucket", "customer-bucket",
			"--base-prefix", "customers/acme",
			"--service-account-key", keyFile,
		},
	}
	for _, args := range commands {
		cmd := newRootCmd("dev")
		cmd.SetArgs(args)
		if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
			t.Fatalf("dari %v: %v", args, err)
		}
	}

	for _, want := range []string{
		"GET /v1/organizations/current/credentials",
		"PUT /v1/organizations/current/credentials/OPENAI_API_KEY",
		"DELETE /v1/organizations/current/credentials/OPENAI_API_KEY",
		"GET /v1/organizations/current/members",
		"POST /v1/organizations/current/invitations",
		"PUT /v1/organizations/current/credentials/GCS_STORAGE_TEAM_KEY",
		"GET /v1/organizations/current/storage-bindings",
		"POST /v1/organizations/current/storage-bindings",
	} {
		if !seen[want] {
			t.Fatalf("did not see request %s", want)
		}
	}
}
