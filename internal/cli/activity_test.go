package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mupt-ai/dari-cli/internal/state"
)

func TestActivityModelsUsesAuthenticatedCurrentOrgRouteAndPreservesFilters(t *testing.T) {
	var gotQuery map[string][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/v1/organizations/current/routing/activity/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		gotQuery = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"from_at": "2026-07-01T00:00:00Z",
			"to_at":   "2026-07-08T00:00:00Z",
			"summary": map[string]any{
				"model_steps":                4,
				"switched_conversation_rate": 0.5,
			},
			"models": []map[string]any{{
				"model":                      "openai/gpt-5.5",
				"provider_cost_per_step_usd": "0.0025",
				"non_completion_rate":        0.25,
			}},
			"transitions": []map[string]any{{"switch_share": 1.0}},
		})
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"--api-url", srv.URL,
		"activity", "models",
		"--from", "2026-07-01T00:00:00Z",
		"--to", "2026-07-08T00:00:00Z",
		"--router-id", "rtr_123",
		"--api-key-id", "oak_1",
		"--api-key-id", "oak_2",
		"--user-id", "usr_123",
		"--model", "openai/gpt-5.5",
		"--model", "anthropic/claude-opus-4-6",
		"--provider", "openai",
		"--status", "provider_error",
	})

	output, err := captureStdoutBytes(t, cmd.Execute)
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := map[string][]string{
		"from":       {"2026-07-01T00:00:00Z"},
		"to":         {"2026-07-08T00:00:00Z"},
		"router_id":  {"rtr_123"},
		"api_key_id": {"oak_1", "oak_2"},
		"user_id":    {"usr_123"},
		"model":      {"openai/gpt-5.5", "anthropic/claude-opus-4-6"},
		"provider":   {"openai"},
		"status":     {"provider_error"},
	}
	if !reflect.DeepEqual(gotQuery, wantQuery) {
		t.Fatalf("query = %#v, want %#v", gotQuery, wantQuery)
	}

	var payload map[string]any
	if err := json.Unmarshal(output, &payload); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, output)
	}
	models := payload["models"].([]any)
	model := models[0].(map[string]any)
	if model["provider_cost_per_step_usd"] != "0.0025" {
		t.Fatalf("provider_cost_per_step_usd = %#v", model["provider_cost_per_step_usd"])
	}
}

func TestActivityModelsExplicitOrganizationUsesBrowserSession(t *testing.T) {
	t.Setenv("DARI_API_KEY", "")
	t.Setenv("DARI_CONFIG_DIR", t.TempDir())
	expiresAt := time.Now().Add(time.Hour).Unix()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/organizations/org_customer/routing/activity/models" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer user-jwt" {
			t.Fatalf("Authorization = %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"summary":     map[string]any{"model_steps": 0},
			"models":      []any{},
			"transitions": []any{},
		})
	}))
	defer srv.Close()

	if err := state.Save(&state.CliState{
		APIURL: srv.URL,
		SupabaseSession: &state.SupabaseSession{
			AccessToken:  "user-jwt",
			RefreshToken: "refresh-token",
			ExpiresAt:    &expiresAt,
		},
		Organizations: map[string]state.Organization{},
	}); err != nil {
		t.Fatal(err)
	}

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"--api-url", srv.URL,
		"activity", "models",
		"--organization-id", "org_customer",
		"--from", "2026-07-01T00:00:00Z",
		"--to", "2026-07-08T00:00:00Z",
	})
	if err := captureStdout(t, cmd.Execute); err != nil {
		t.Fatal(err)
	}
}

func TestActivityModelsExplicitOrganizationRejectsManagementKey(t *testing.T) {
	useTestAPIKey(t)
	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"activity", "models",
		"--organization-id", "org_customer",
		"--from", "2026-07-01T00:00:00Z",
		"--to", "2026-07-08T00:00:00Z",
	})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "requires browser login") {
		t.Fatalf("error = %v", err)
	}
}

func TestActivityModelsRejectsInvalidRangeAndStatusBeforeRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "invalid timestamp",
			args: []string{"activity", "models", "--from", "yesterday", "--to", "2026-07-08T00:00:00Z"},
			want: "invalid --from",
		},
		{
			name: "reversed range",
			args: []string{"activity", "models", "--from", "2026-07-08T00:00:00Z", "--to", "2026-07-01T00:00:00Z"},
			want: "--to must be later",
		},
		{
			name: "invalid status",
			args: []string{"activity", "models", "--from", "2026-07-01T00:00:00Z", "--to", "2026-07-08T00:00:00Z", "--status", "failed"},
			want: "invalid --status",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cmd := newRootCmd("dev")
			cmd.SetArgs(test.args)
			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestActivityModelsAcceptsFractionalSecondTimestamps(t *testing.T) {
	flags := activityModelsFlags{
		from: "2026-07-01T00:00:00.000Z",
		to:   "2026-07-08T12:30:45.123456789+02:00",
	}
	query, err := flags.query()
	if err != nil {
		t.Fatal(err)
	}
	if got := query.Get("from"); got != "2026-07-01T00:00:00Z" {
		t.Fatalf("from = %q", got)
	}
	if got := query.Get("to"); got != "2026-07-08T12:30:45.123456789+02:00" {
		t.Fatalf("to = %q", got)
	}
}

func captureStdoutBytes(t *testing.T, run func() error) ([]byte, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := run()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return output, runErr
}
