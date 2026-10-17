package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestClaudeCommandUsesSelectedRouter(t *testing.T) {
	for _, routerID := range []string{"", "default", "rtr_custom"} {
		t.Run(routerID, func(t *testing.T) {
			command, err := buildAgentCommand("/claude", agentLaunch{name: "claude"}, "test-key", routerID)
			if err != nil {
				t.Fatal(err)
			}
			if command.cleanup != nil {
				defer command.cleanup()
			}
			want := routingBaseURL
			if routerID == "rtr_custom" {
				want += "/rtr_custom"
			}
			if got := environmentValue(command.env, "ANTHROPIC_BASE_URL"); got != want {
				t.Fatalf("base URL = %q, want %q", got, want)
			}
		})
	}
}

func TestBuildClaudeAgentCommandUsesTemporaryStatusLine(t *testing.T) {
	command, err := buildAgentCommand(
		"/claude",
		agentLaunch{name: "claude", args: []string{"--print", "review"}},
		"dari_route",
		"default",
	)
	if err != nil {
		t.Fatal(err)
	}
	if command.cleanup == nil {
		t.Fatal("Claude command has no cleanup")
	}

	settingsIndex := slices.Index(command.args, "--settings")
	if settingsIndex < 0 || settingsIndex+1 >= len(command.args) {
		t.Fatalf("args do not contain --settings: %q", command.args)
	}
	settingsPath := command.args[settingsIndex+1]
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	if settings.StatusLine.Type != "command" {
		t.Fatalf("statusLine.type = %q", settings.StatusLine.Type)
	}
	if !strings.Contains(settings.StatusLine.Command, " agent-status --claude") {
		t.Fatalf("status command = %q", settings.StatusLine.Command)
	}
	if strings.Contains(string(data), "dari_route") {
		t.Fatal("Claude status settings contain the secret key")
	}

	command.cleanup()
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("temporary settings still exist: %v", err)
	}
}

func TestBuildClaudeAgentCommandSkipsStatusLineWhenSettingsFlagPresent(t *testing.T) {
	command, err := buildAgentCommand(
		"/claude",
		agentLaunch{name: "claude", args: []string{"--settings", "mine.json"}},
		"dari_route",
		"default",
	)
	if err != nil {
		t.Fatal(err)
	}
	if command.cleanup != nil {
		t.Fatal("Claude command should not own cleanup when --settings is already set")
	}
	if slices.Index(command.args, "--settings") != 0 || command.args[1] != "mine.json" {
		t.Fatalf("args = %q", command.args)
	}
}

func TestClaudeStatusLineReadsDariRouting(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	lines := []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user"}},
		{
			"type": "assistant",
			"message": map[string]any{
				"model": "xai/grok-4.6",
				"dari_routing": map[string]any{
					"selected_model":        "xai/grok-4.6",
					"reasoning_effort":      "high",
					"lease_turns_remaining": 1,
				},
			},
		},
	}
	var body strings.Builder
	for _, line := range lines {
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(encoded)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(transcript, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	stdin, err := json.Marshal(map[string]string{"transcript_path": transcript})
	if err != nil {
		t.Fatal(err)
	}
	root := newRootCmd("test")
	var out strings.Builder
	root.SetIn(strings.NewReader(string(stdin)))
	root.SetOut(&out)
	root.SetArgs([]string{"agent-status", "--claude"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	want := "xai/grok-4.6 · high · lease 2 turns left"
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
