package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestProviderCredentialCommandsSendTypedPayloads(t *testing.T) {
	t.Setenv("TEST_AWS_ACCESS_KEY_ID", "AKIATEST")
	t.Setenv("TEST_AWS_SECRET_ACCESS_KEY", "secret")
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
		}
		requests = append(requests, map[string]any{
			"method": r.Method,
			"path":   r.URL.Path,
			"body":   body,
		})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "cred_test"})
	}))
	defer srv.Close()
	useTestAPIKey(t)

	commands := [][]string{
		{"--api-url", srv.URL, "credentials", "provider", "add", "openrouter", "OpenRouter Production", "sk-test"},
		{"--api-url", srv.URL, "credentials", "provider", "update", "cred_aws", "Bedrock Production", "--aws-region", "us-east-1", "--aws-access-key-id-env", "TEST_AWS_ACCESS_KEY_ID", "--aws-secret-access-key-env", "TEST_AWS_SECRET_ACCESS_KEY"},
		{"--api-url", srv.URL, "credentials", "provider", "update", "cred_aws", "Bedrock Production", "--aws-region", "us-west-2"},
		{"--api-url", srv.URL, "credentials", "provider", "remove", "cred_old"},
	}
	for _, args := range commands {
		cmd := newRootCmd("dev")
		cmd.SetArgs(args)
		if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
			t.Fatalf("dari %v: %v", args, err)
		}
	}

	want := []map[string]any{
		{
			"method": http.MethodPost,
			"path":   "/v1/organizations/current/credentials/provider-credentials",
			"body": map[string]any{
				"provider": "openrouter",
				"label":    "OpenRouter Production",
				"auth": map[string]any{
					"type":    "api_key",
					"api_key": "sk-test",
				},
			},
		},
		{
			"method": http.MethodPut,
			"path":   "/v1/organizations/current/credentials/provider-credentials/cred_aws",
			"body": map[string]any{
				"label": "Bedrock Production",
				"auth": map[string]any{
					"type":              "aws_sigv4",
					"region":            "us-east-1",
					"access_key_id":     "AKIATEST",
					"secret_access_key": "secret",
				},
			},
		},
		{
			"method": http.MethodPut,
			"path":   "/v1/organizations/current/credentials/provider-credentials/cred_aws",
			"body": map[string]any{
				"label":  "Bedrock Production",
				"region": "us-west-2",
			},
		},
		{
			"method": http.MethodDelete,
			"path":   "/v1/organizations/current/credentials/provider-credentials/cred_old",
			"body":   map[string]any(nil),
		},
	}
	if !reflect.DeepEqual(requests, want) {
		t.Fatalf("requests = %#v, want %#v", requests, want)
	}
}

func TestRouterCreateSendsSavedProviderCredential(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_new"})
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"--api-url", srv.URL, "router", "create", "Production",
		"--model", "openrouter/openai/gpt-5.5",
		"--provider-credential", "openrouter=cred_openrouter",
	})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router create: %v", err)
	}

	want := map[string]any{
		"name":                    "Production",
		"enabled_models":          []any{"openrouter/openai/gpt-5.5"},
		"provider_credential_ids": map[string]any{"openrouter": "cred_openrouter"},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("create body = %#v, want %#v", body, want)
	}
}

func TestRouterManifestAcceptsSavedProviderCredentialWithoutSource(t *testing.T) {
	body, err := (routerCreateManifest{
		Name:                  "Production",
		EnabledModels:         []string{"openrouter/openai/gpt-5.5"},
		ProviderCredentialIDs: map[string]string{"OpenRouter": "cred_openrouter"},
	}).createRequest("router.yml", nil)
	if err != nil {
		t.Fatalf("createRequest: %v", err)
	}
	if !reflect.DeepEqual(body.ProviderCredentialIDs, map[string]string{"openrouter": "cred_openrouter"}) {
		t.Fatalf("provider credential IDs = %#v", body.ProviderCredentialIDs)
	}
	if body.ProviderKeySources != nil {
		t.Fatalf("provider key sources = %#v, want nil", body.ProviderKeySources)
	}
}
