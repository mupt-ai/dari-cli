package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestParseAgentLaunch(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantArgs []string
		wantOK   bool
	}{
		{
			name:     "claude forwards every remaining token",
			args:     []string{"--claude", "--print", "--", "-prompt"},
			wantName: "claude",
			wantArgs: []string{"--print", "--", "-prompt"},
			wantOK:   true,
		},
		{
			name:     "codex",
			args:     []string{"--codex", "exec", "fix tests"},
			wantName: "codex",
			wantArgs: []string{"exec", "fix tests"},
			wantOK:   true,
		},
		{
			name:     "pi",
			args:     []string{"--pi", "-p", "review"},
			wantName: "pi",
			wantArgs: []string{"-p", "review"},
			wantOK:   true,
		},
		{
			name:   "ordinary Dari command",
			args:   []string{"router", "list"},
			wantOK: false,
		},
		{
			name:   "selector must be first",
			args:   []string{"--api-url", "https://example.test", "--claude"},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseAgentLaunch(tt.args)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got.name != tt.wantName {
				t.Errorf("name = %q, want %q", got.name, tt.wantName)
			}
			if !slices.Equal(got.args, tt.wantArgs) {
				t.Errorf("args = %q, want %q", got.args, tt.wantArgs)
			}
		})
	}
}

func TestAgentExecutableUsesManagedPathOverride(t *testing.T) {
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("DARI_CLAUDE_PATH", path)

	got, err := agentExecutable("claude")
	if err != nil {
		t.Fatal(err)
	}
	if got != path {
		t.Fatalf("agentExecutable() = %q, want %q", got, path)
	}
}

func TestAgentExecutableReportsInvalidManagedPathOverride(t *testing.T) {
	t.Setenv("DARI_CLAUDE_PATH", t.TempDir()+"/missing")

	_, err := agentExecutable("claude")
	if err == nil || !strings.Contains(err.Error(), "DARI_CLAUDE_PATH does not point to an executable file") {
		t.Fatalf("agentExecutable() error = %v", err)
	}
}

func TestBuildAgentCommandKeepsDariSettingsLast(t *testing.T) {
	tests := []struct {
		name       string
		launch     agentLaunch
		wantSuffix []string
		wantEnv    map[string]string
	}{
		{
			name: "claude",
			launch: agentLaunch{
				name: "claude",
				args: []string{"--model", "opus", "--print", "hello"},
			},
			wantSuffix: []string{"--model", routingModel},
			wantEnv: map[string]string{
				"ANTHROPIC_BASE_URL":             routingBaseURL,
				"ANTHROPIC_AUTH_TOKEN":           "dari_route",
				"ANTHROPIC_API_KEY":              "",
				"CLAUDE_CODE_EFFORT_LEVEL":       "auto",
				"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",
				"CLAUDE_CODE_SUBAGENT_MODEL":     routingModel,
			},
		},
		{
			name: "codex",
			launch: agentLaunch{
				name: "codex",
				args: []string{"exec", "-c", `model="other"`, "hello"},
			},
			wantSuffix: []string{
				"--config", `model="dari/routing"`,
				"--config", `model_provider="dari"`,
				"--config", `model_reasoning_summary="none"`,
				"--config", `model_providers.dari.name="Dari"`,
				"--config", `model_providers.dari.base_url="https://routing.dari.dev/v1"`,
				"--config", `model_providers.dari.env_key="DARI_ROUTING_API_KEY"`,
				"--config", `model_providers.dari.wire_api="responses"`,
			},
			wantEnv: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := buildAgentCommand("/agent", tt.launch, "dari_route")
			if err != nil {
				t.Fatal(err)
			}
			if command.cleanup != nil {
				defer command.cleanup()
			}
			if !slices.Equal(command.args[len(command.args)-len(tt.wantSuffix):], tt.wantSuffix) {
				t.Errorf("args = %q, want suffix %q", command.args, tt.wantSuffix)
			}
			if got := environmentValue(command.env, routingAPIKeyEnv); got != "dari_route" {
				t.Errorf("%s = %q", routingAPIKeyEnv, got)
			}
			for name, want := range tt.wantEnv {
				if got := environmentValue(command.env, name); got != want {
					t.Errorf("%s = %q, want %q", name, got, want)
				}
			}
		})
	}
}

func TestBuildAgentCommandInsertsSettingsBeforeDoubleDash(t *testing.T) {
	command, err := buildAgentCommand(
		"/claude",
		agentLaunch{name: "claude", args: []string{"--print", "--", "--help"}},
		"dari_route",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--print", "--model", routingModel, "--", "--help"}
	if !slices.Equal(command.args, want) {
		t.Fatalf("args = %q, want %q", command.args, want)
	}
	if onlyRequestsAgentInfo([]string{"--", "--help"}) {
		t.Fatal("prompt after -- was mistaken for the agent help flag")
	}
}

func TestBuildPiAgentCommandUsesTemporaryProviderExtension(t *testing.T) {
	command, err := buildAgentCommand(
		"/pi",
		agentLaunch{name: "pi", args: []string{"-p", "review"}},
		"dari_route",
	)
	if err != nil {
		t.Fatal(err)
	}
	if command.cleanup == nil {
		t.Fatal("Pi command has no cleanup")
	}

	extensionIndex := slices.Index(command.args, "--extension")
	if extensionIndex < 0 || extensionIndex+1 >= len(command.args) {
		t.Fatalf("args do not contain an extension: %q", command.args)
	}
	path := command.args[extensionIndex+1]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `pi.registerProvider("dari"`) {
		t.Errorf("extension does not register Dari: %s", content)
	}
	if strings.Contains(content, "dari_route") {
		t.Fatal("extension contains the secret key")
	}

	command.cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary extension still exists: %v", err)
	}
}

func TestResolveAgentRoutingKeyCreatesAndReusesKey(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/v1/organizations/current/api-keys" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer dari_management" {
			t.Errorf("Authorization = %q", got)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["key_type"] != "routing" {
			t.Errorf("key_type = %q", body["key_type"])
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"api_key": "dari_routing"})
	}))
	defer server.Close()

	t.Setenv("DARI_CONFIG_DIR", t.TempDir())
	t.Setenv("DARI_API_URL", server.URL)
	t.Setenv("DARI_API_KEY", "dari_management")
	t.Setenv(routingAPIKeyEnv, "")

	for range 2 {
		key, err := resolveAgentRoutingKey(context.Background(), strings.NewReader(""), io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		if key != "dari_routing" {
			t.Errorf("key = %q", key)
		}
	}
	if requests != 1 {
		t.Fatalf("API requests = %d, want 1", requests)
	}
}

func TestResolveAgentRoutingKeyPrefersEnvironment(t *testing.T) {
	t.Setenv(routingAPIKeyEnv, " dari_explicit ")
	key, err := resolveAgentRoutingKey(context.Background(), strings.NewReader(""), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if key != "dari_explicit" {
		t.Fatalf("key = %q", key)
	}
}

func TestRootHelpListsAgentLaunchers(t *testing.T) {
	cmd := newRootCmd("dev")
	var output strings.Builder
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--claude", "--codex", "--pi"} {
		if !strings.Contains(output.String(), flag) {
			t.Errorf("help does not contain %s", flag)
		}
	}
}

func environmentValue(env []string, name string) string {
	prefix := name + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
