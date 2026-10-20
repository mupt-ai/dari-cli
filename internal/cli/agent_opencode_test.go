package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildOpenCodeCommandInjectsSessionConfig(t *testing.T) {
	command, err := buildAgentCommand("/opencode", agentLaunch{name: "opencode"}, "dari_route", "router_1")
	if err != nil {
		t.Fatal(err)
	}
	if command.cleanup == nil {
		t.Fatal("OpenCode command has no cleanup")
	}
	if !slices.Equal(command.args, []string{"--standalone"}) {
		t.Fatalf("args = %q", command.args)
	}
	raw := environmentValue(command.env, "OPENCODE_CONFIG_CONTENT")
	if strings.Contains(raw, "dari_route") {
		t.Fatal("config contains the secret key")
	}
	var config struct {
		Model      string                       `json:"model"`
		SmallModel string                       `json:"small_model"`
		Agent      map[string]map[string]string `json:"agent"`
		Plugins    []string                     `json:"plugins"`
		Provider   map[string]struct {
			Options map[string]string `json:"options"`
			Models  map[string]struct {
				ID         string              `json:"id"`
				Attachment bool                `json:"attachment"`
				Limit      map[string]int      `json:"limit"`
				Modalities map[string][]string `json:"modalities"`
			} `json:"models"`
		} `json:"provider"`
	}
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		t.Fatal(err)
	}
	if config.Model != routingModel || config.SmallModel != routingModel {
		t.Errorf("model = %q, small_model = %q", config.Model, config.SmallModel)
	}
	for _, name := range []string{"build", "plan", "general", "explore", "scout", "title", "summary", "compaction"} {
		if config.Agent[name]["model"] != routingModel {
			t.Errorf("agent %s model = %q", name, config.Agent[name]["model"])
		}
	}
	dari := config.Provider["dari"]
	if dari.Options["baseURL"] != "https://routing.dari.dev/router_1/v1" {
		t.Errorf("baseURL = %q", dari.Options["baseURL"])
	}
	if dari.Options["apiKey"] != "{env:DARI_ROUTING_API_KEY}" {
		t.Errorf("apiKey = %q", dari.Options["apiKey"])
	}
	model := dari.Models["routing"]
	if model.ID != routingModel {
		t.Errorf("model id = %q", model.ID)
	}
	if !model.Attachment || !slices.Contains(model.Modalities["input"], "image") {
		t.Errorf("model does not accept images: %+v", model)
	}
	if model.Limit["context"] != 1_000_000 {
		t.Errorf("context = %d", model.Limit["context"])
	}

	if len(config.Plugins) != 1 {
		t.Fatalf("plugins = %q", config.Plugins)
	}
	plugin := config.Plugins[0]
	server, err := os.ReadFile(filepath.Join(plugin, "server.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`ctx.session.hook("model.request"`, `"x-opencode-request-kind"`, `"x-opencode-agent"`, `ctx.session.hook("http.response"`, `"x-dari-selected-model"`, `"agent-status", "--headers"`, `providerID: "dari"`} {
		if !strings.Contains(string(server), want) {
			t.Errorf("server plugin does not record routing status (%s missing)", want)
		}
	}
	if strings.Contains(string(server), "DARI_STATUS_EXECUTABLE") {
		t.Error("server plugin does not embed the status executable")
	}
	tui, err := os.ReadFile(filepath.Join(plugin, "tui.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tui), `append: "prompt.footer.status"`) {
		t.Errorf("TUI plugin does not show routing status: %s", tui)
	}

	command.cleanup()
	if _, err := os.Stat(filepath.Dir(plugin)); !os.IsNotExist(err) {
		t.Fatalf("temporary plugin still exists: %v", err)
	}
}

func TestBuildOpenCodeCommandArguments(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"directory", []string{"./repo"}, []string{"./repo", "--standalone"}},
		{"run replaces user model", []string{"run", "-m", "openai/gpt", "fix it"}, []string{"run", "fix it", "--standalone", "--model", routingModel}},
		{"run keeps prompt after double dash", []string{"run", "--", "--help"}, []string{"run", "--standalone", "--model", routingModel, "--", "--help"}},
		{"mini replaces user model", []string{"mini", "--model=openai/gpt"}, []string{"mini", "--standalone", "--model", routingModel}},
		{"explicit standalone", []string{"--standalone", "--auto"}, []string{"--standalone", "--auto"}},
		{"mini", []string{"mini"}, []string{"mini", "--standalone", "--model", routingModel}},
		{"models command does not get model flag", []string{"models"}, []string{"models", "--standalone"}},
		{"stats command does not get model flag", []string{"stats"}, []string{"stats", "--standalone"}},
		{"api uses private server without model flag", []string{"api", "GET", "/session"}, []string{"api", "GET", "/session", "--standalone"}},
		{"leading flag before models", []string{"--print-logs", "models"}, []string{"--print-logs", "models", "--standalone"}},
		{"leading valued flag before serve", []string{"--log-level", "debug", "serve"}, []string{"--log-level", "debug", "serve"}},
		{"leading valued flag before run", []string{"--log-level", "debug", "run", "hi"}, []string{"--log-level", "debug", "run", "hi", "--standalone", "--model", routingModel}},
		{"interactive session value is not a subcommand", []string{"-s", "ses_1"}, []string{"-s", "ses_1", "--standalone"}},
		{"serve inherits environment", []string{"serve"}, []string{"serve"}},
		{"help", []string{"--help"}, []string{"--help"}},
		{"version", []string{"run", "-v"}, []string{"run", "-v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command, err := buildAgentCommand("/opencode", agentLaunch{name: "opencode", args: tt.args}, "dari_route", "default")
			if err != nil {
				t.Fatal(err)
			}
			defer command.cleanup()
			if !slices.Equal(command.args, tt.want) {
				t.Fatalf("args = %q, want %q", command.args, tt.want)
			}
			if environmentValue(command.env, "OPENCODE_CONFIG_CONTENT") == "" {
				t.Fatal("OpenCode config was not injected")
			}
		})
	}
}

func TestBuildOpenCodeManagementCommandsGetNoDariConfig(t *testing.T) {
	for _, args := range [][]string{{"service", "start"}, {"session", "list"}, {"--print-logs", "service", "restart"}} {
		command, err := buildAgentCommand("/opencode", agentLaunch{name: "opencode", args: args}, "dari_route", "default")
		if err != nil {
			t.Fatal(err)
		}
		if command.cleanup != nil {
			t.Errorf("%q wrote a temporary plugin", args)
		}
		if !slices.Equal(command.args, args) {
			t.Errorf("args = %q, want %q", command.args, args)
		}
		for _, name := range []string{"OPENCODE_CONFIG_CONTENT", routingAPIKeyEnv} {
			if environmentValue(command.env, name) != "" {
				t.Errorf("%q passes %s to the shared service", args, name)
			}
		}
	}
}

func TestBuildOpenCodeCommandRejectsPairing(t *testing.T) {
	_, err := buildAgentCommand("/opencode", agentLaunch{name: "opencode", args: []string{"--print-logs", "pair"}}, "dari_route", "default")
	if err == nil || !strings.Contains(err.Error(), "dari opencode serve") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildOpenCodeCommandRejectsSharedServer(t *testing.T) {
	_, err := buildAgentCommand("/opencode", agentLaunch{name: "opencode", args: []string{"--server", "http://127.0.0.1:4096"}}, "dari_route", "default")
	if err == nil || !strings.Contains(err.Error(), "remove --server") {
		t.Fatalf("error = %v", err)
	}
}
