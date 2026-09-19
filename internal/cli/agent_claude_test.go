package cli

import (
	"encoding/json"
	"os"
	"os/exec"
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
	script, err := os.ReadFile(settings.StatusLine.Command)
	if err != nil {
		t.Fatal(err)
	}
	content := string(script)
	for _, want := range []string{
		"dari_routing",
		"selected_model",
		"lease_turns_remaining",
		"transcript_path",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("status line script does not read routing (%s missing)", want)
		}
	}
	if strings.Contains(content, "dari_route") || strings.Contains(string(data), "dari_route") {
		t.Fatal("Claude status files contain the secret key")
	}

	command.cleanup()
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("temporary settings still exist: %v", err)
	}
	if _, err := os.Stat(settings.StatusLine.Command); !os.IsNotExist(err) {
		t.Fatalf("temporary status script still exists: %v", err)
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

func TestClaudeStatusLineScriptReadsDariRouting(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	command, err := buildAgentCommand("/claude", agentLaunch{name: "claude"}, "dari_route", "default")
	if err != nil {
		t.Fatal(err)
	}
	defer command.cleanup()

	settingsIndex := slices.Index(command.args, "--settings")
	data, err := os.ReadFile(command.args[settingsIndex+1])
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}

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
	cmd := exec.Command("python3", settings.StatusLine.Command)
	cmd.Stdin = strings.NewReader(string(stdin))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("status line script: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "xai/grok-4.6 · high · lease 2 turns left"
	if got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}
