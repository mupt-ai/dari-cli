package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestPrepareCodexCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected catalog request")
		}
		_, _ = w.Write([]byte(`{"models":[{"slug":"dari/routing","context_window":272000,"auto_review_model_override":null,"support_verbosity":false,"web_search_tool_type":null,"tool_mode":null,"multi_agent_version":null}]}`))
	}))
	defer server.Close()
	command := agentCommand{args: []string{"-c", `approvals_reviewer="auto_review"`, "--", "prompt"}}
	if err := prepareCodexCatalog(context.Background(), &command, "test-key", server.URL); err != nil {
		t.Fatal(err)
	}
	defer command.cleanup()
	if command.args[1] != `approvals_reviewer="auto_review"` || command.args[6] != "--" || command.args[5] != `web_search="disabled"` {
		t.Fatalf("args: %v", command.args)
	}
	path, err := strconv.Unquote(strings.TrimPrefix(command.args[3], "model_catalog_json="))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions: %v", info.Mode())
	}
	var catalog struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(data, &catalog); err != nil {
		t.Fatal(err)
	}
	model := catalog.Models[0]
	if model["auto_review_model_override"] != routingModel || model["context_window"] != float64(272000) || model["support_verbosity"] != false {
		t.Fatalf("model: %v", model)
	}
	if model["web_search_tool_type"] != "text" {
		t.Fatalf("invalid web search type: %v", model)
	}
	for _, field := range []string{"tool_mode", "multi_agent_version"} {
		if _, ok := model[field]; ok {
			t.Fatalf("null selector retained: %s", field)
		}
	}
	command.cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("catalog not cleaned up: %v", err)
	}
}

func TestPrepareCodexCatalogFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"unauthorized", "secret response body", 401},
		{"invalid json", "invalid", 200},
		{"missing router", `{"models":[{"slug":"other"}]}`, 200},
		{"oversized", strings.Repeat("x", (4<<20)+1), 200},
		{"redirect", "", 302},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			command := agentCommand{}
			err := prepareCodexCatalog(context.Background(), &command, "test-key", server.URL)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error(), "secret response body") || strings.Contains(err.Error(), "test-key") {
				t.Fatalf("leaked response or key: %v", err)
			}
			if command.cleanup != nil || len(command.args) != 0 {
				t.Fatal("modified command on failure")
			}
		})
	}
}

func TestCodexCommandUsesSelectedRouter(t *testing.T) {
	for _, routerID := range []string{"", "default", "rtr_custom"} {
		t.Run(routerID, func(t *testing.T) {
			command, err := buildAgentCommand("/codex", agentLaunch{name: "codex"}, "test-key", routerID)
			if err != nil {
				t.Fatal(err)
			}
			want := routingBaseURL
			if routerID == "rtr_custom" {
				want += "/rtr_custom"
			}
			want = `model_providers.dari.base_url="` + want + `/v1"`
			found := false
			for _, arg := range command.args {
				if arg == want {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %q in %v", want, command.args)
			}
		})
	}
}
