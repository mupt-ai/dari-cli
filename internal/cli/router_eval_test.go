package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestRouterCommandsUseAPIKeyRoutes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/organizations/current/routers":
			_ = json.NewEncoder(w).Encode(map[string]any{"routers": []any{map[string]any{"id": "rtr_123"}}})
		case "/v1/organizations/current/routers/rtr_123":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123", "name": "Production"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

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
		"GET /v1/organizations/current/routers",
		"GET /v1/organizations/current/routers/rtr_123",
	} {
		if !seen[want] {
			t.Fatalf("did not see request %s", want)
		}
	}
}

func TestRouterModelsUsesModelCatalogRoute(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"groups": []any{}})
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{"--api-url", srv.URL, "router", "models"})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router models: %v", err)
	}
	if !seen["GET /v1/organizations/current/routers/model-catalog"] {
		t.Fatalf("did not see model-catalog request; saw %v", seen)
	}
}

func TestRouterCreateSendsPayload(t *testing.T) {
	t.Setenv("TEST_OPENROUTER_KEY", "sk-or-env")
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/current/routers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
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
		"--api-url", srv.URL, "router", "create", "Production Router",
		"--model", "openrouter/openai/gpt-5,openrouter/anthropic/claude-sonnet-4.5",
		"--provider-key-env", "openrouter=TEST_OPENROUTER_KEY",
		"--eval", "eval_123",
		"--strategy", "heuristic",
		"--performance-weight", "0.7",
		"--price-weight", "0.3",
		"--eval-weight", "eval_123=1.0",
	})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router create: %v", err)
	}

	want := map[string]any{
		"name":             "Production Router",
		"enabled_models":   []any{"openrouter/openai/gpt-5", "openrouter/anthropic/claude-sonnet-4.5"},
		"provider_keys":    map[string]any{"openrouter": "sk-or-env"},
		"eval_ids":         []any{"eval_123"},
		"routing_strategy": "heuristic",
		"heuristic_config": map[string]any{
			"performance_weight": 0.7,
			"price_weight":       0.3,
			"eval_weights":       map[string]any{"eval_123": 1.0},
		},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("create body = %#v, want %#v", body, want)
	}
}

func TestRouterCreateInfersHeuristicStrategyFromWeights(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/organizations/current/routers" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
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
		"--api-url", srv.URL, "router", "create", "Heuristic Router",
		"--model", "openrouter/openai/gpt-5",
		"--managed-key", "openrouter",
		"--eval", "eval_123",
		"--performance-weight", "0.7",
		"--price-weight", "0.3",
		"--eval-weight", "eval_123=1.0",
	})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router create: %v", err)
	}
	if body["routing_strategy"] != "heuristic" {
		t.Fatalf("routing_strategy = %#v, want heuristic", body["routing_strategy"])
	}
}

func TestRouterCreateRequiresModels(t *testing.T) {
	useTestAPIKey(t)
	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{"router", "create", "NoModels"})
	cmd.SetErr(io.Discard)
	if err := captureStdout(t, func() error { return cmd.Execute() }); err == nil {
		t.Fatal("expected error when --model is missing")
	}
}

func TestRouterCreateRejectsInvalidHeuristicWeights(t *testing.T) {
	useTestAPIKey(t)
	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"router", "create", "BadWeights",
		"--model", "openrouter/openai/gpt-5",
		"--performance-weight", "0.7",
		"--price-weight", "0.3",
		"--eval-weight", "eval_123=1abc",
	})
	cmd.SetErr(io.Discard)
	if err := captureStdout(t, func() error { return cmd.Execute() }); err == nil {
		t.Fatal("expected error for invalid eval weight")
	}
}

func TestRouterCreateRequiresHeuristicConfigForHeuristicStrategy(t *testing.T) {
	useTestAPIKey(t)
	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"router", "create", "MissingConfig",
		"--model", "openrouter/openai/gpt-5",
		"--managed-key", "openrouter",
		"--strategy", "heuristic",
	})
	cmd.SetErr(io.Discard)
	if err := captureStdout(t, func() error { return cmd.Execute() }); err == nil {
		t.Fatal("expected error when heuristic strategy has no config")
	}
}

func TestRouterCreateRejectsEvalWeightsThatDoNotSum(t *testing.T) {
	useTestAPIKey(t)
	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"router", "create", "BadEvalWeights",
		"--model", "openrouter/openai/gpt-5",
		"--managed-key", "openrouter",
		"--eval", "eval_1,eval_2",
		"--performance-weight", "0.7",
		"--price-weight", "0.3",
		"--eval-weight", "eval_1=0.2",
		"--eval-weight", "eval_2=0.2",
	})
	cmd.SetErr(io.Discard)
	if err := captureStdout(t, func() error { return cmd.Execute() }); err == nil {
		t.Fatal("expected error when eval weights do not sum to 1")
	}
}

func TestRouterUpdateOverlaysCurrentConfig(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/organizations/current/routers/rtr_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             "rtr_123",
				"name":           "Production",
				"enabled_models": []string{"openrouter/openai/gpt-5"},
			})
		case "PUT /v1/organizations/current/routers/rtr_123":
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"--api-url", srv.URL, "router", "update", "rtr_123",
		"--name", "Staging",
		"--managed-key", "openrouter",
		"--clear-evals",
	})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router update: %v", err)
	}

	want := map[string]any{
		"name":                 "Staging",
		"enabled_models":       []any{"openrouter/openai/gpt-5"},
		"provider_key_sources": map[string]any{"openrouter": "managed"},
		"eval_ids":             []any{},
	}
	if !reflect.DeepEqual(body, want) {
		t.Fatalf("update body = %#v, want %#v", body, want)
	}
}

func TestRouterUpdatePreservesHeuristicEvalWeights(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/organizations/current/routers/rtr_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":               "rtr_123",
				"name":             "Production",
				"enabled_models":   []string{"openrouter/openai/gpt-5"},
				"routing_strategy": "heuristic",
				"evals":            []map[string]any{{"id": "eval_123"}},
				"heuristic_config": map[string]any{
					"performance_weight": 0.6,
					"price_weight":       0.4,
					"eval_weights":       map[string]any{"eval_123": 1.0},
				},
			})
		case "PUT /v1/organizations/current/routers/rtr_123":
			raw, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(raw, &body); err != nil {
				t.Fatalf("decode body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{
		"--api-url", srv.URL, "router", "update", "rtr_123",
		"--performance-weight", "0.7",
		"--price-weight", "0.3",
	})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router update: %v", err)
	}
	want := map[string]any{
		"performance_weight": 0.7,
		"price_weight":       0.3,
		"eval_weights":       map[string]any{"eval_123": 1.0},
	}
	if !reflect.DeepEqual(body["heuristic_config"], want) {
		t.Fatalf("heuristic_config = %#v, want %#v", body["heuristic_config"], want)
	}
}

func TestRouterUpdateRequiresHeuristicConfigWhenEvalsChange(t *testing.T) {
	seenPut := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/organizations/current/routers/rtr_123":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":               "rtr_123",
				"name":             "Production",
				"enabled_models":   []string{"openrouter/openai/gpt-5"},
				"routing_strategy": "heuristic",
				"evals":            []map[string]any{{"id": "eval_123"}},
				"heuristic_config": map[string]any{
					"performance_weight": 0.6,
					"price_weight":       0.4,
					"eval_weights":       map[string]any{"eval_123": 1.0},
				},
			})
		case "PUT /v1/organizations/current/routers/rtr_123":
			seenPut = true
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123"})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{"--api-url", srv.URL, "router", "update", "rtr_123", "--clear-evals"})
	cmd.SetErr(io.Discard)
	if err := captureStdout(t, func() error { return cmd.Execute() }); err == nil {
		t.Fatal("expected error when heuristic evals change without config")
	}
	if seenPut {
		t.Fatal("unexpected update request after local validation failed")
	}
}

func TestRouterDeleteSkipsConfirmWithYes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "rtr_123", "deleted_at": "2026-06-12T00:00:00Z"})
	}))
	defer srv.Close()
	useTestAPIKey(t)

	cmd := newRootCmd("dev")
	cmd.SetArgs([]string{"--api-url", srv.URL, "router", "delete", "https://routing.dari.dev/rtr_123/chat/completions", "--yes"})
	if err := captureStdout(t, func() error { return cmd.Execute() }); err != nil {
		t.Fatalf("dari router delete: %v", err)
	}
	if !seen["DELETE /v1/organizations/current/routers/rtr_123"] {
		t.Fatalf("did not see delete request; saw %v", seen)
	}
}

func TestEvalCommandsUseAPIKeyRoutes(t *testing.T) {
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer dari_test" {
			t.Fatalf("Authorization = %q", auth)
		}
		seen[r.Method+" "+r.URL.Path] = true
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/organizations/current/evals":
			_ = json.NewEncoder(w).Encode(map[string]any{"evals": []any{map[string]any{"id": "eval_123"}}})
		case "/v1/organizations/current/evals/eval_123":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "eval_123", "name": "Code quality"})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	useTestAPIKey(t)

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
		"GET /v1/organizations/current/evals",
		"GET /v1/organizations/current/evals/eval_123",
	} {
		if !seen[want] {
			t.Fatalf("did not see request %s", want)
		}
	}
}
