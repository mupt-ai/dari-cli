package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
)

type apiKeyCreateRequest struct {
	Label  string   `json:"label"`
	Scopes []string `json:"scopes"`
}

func TestAPIKeysCreateScopes(t *testing.T) {
	tests := []struct {
		name      string
		scopeArgs []string
		want      []string
	}{
		{
			name: "default platform scope",
			want: []string{"platform"},
		},
		{
			name:      "routing scope",
			scopeArgs: []string{"--scope", "routing"},
			want:      []string{"routing"},
		},
		{
			name:      "repeated scopes",
			scopeArgs: []string{"--scope", "platform", "--scope", "routing"},
			want:      []string{"platform", "routing"},
		},
		{
			name:      "comma-separated scopes",
			scopeArgs: []string{"--scope", "platform,routing"},
			want:      []string{"platform", "routing"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runAPIKeysCreate(t, tt.scopeArgs)
			if got.Label != "Router client" {
				t.Fatalf("label = %q", got.Label)
			}
			if !slices.Equal(got.Scopes, tt.want) {
				t.Fatalf("scopes = %#v, want %#v", got.Scopes, tt.want)
			}
		})
	}
}

func runAPIKeysCreate(t *testing.T, scopeArgs []string) apiKeyCreateRequest {
	t.Helper()

	var got apiKeyCreateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v1/organizations/org_123/api-keys" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer dak_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"id":     "key_123",
			"label":  got.Label,
			"scopes": got.Scopes,
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	defer srv.Close()

	t.Setenv("DARI_API_KEY", "dak_test")
	t.Setenv("DARI_ORG_ID", "org_123")

	cmd := newRootCmd("dev")
	args := []string{"--api-url", srv.URL, "api-keys", "create", "--name", "Router client"}
	args = append(args, scopeArgs...)
	cmd.SetArgs(args)

	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatal(err)
	}
	return got
}

func captureStdout(t *testing.T, fn func() error) error {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(r)
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	return runErr
}
