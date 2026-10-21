package claude

import (
	"encoding/json"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestBuildClaudeAgentCommandUsesTemporaryStatusLine(t *testing.T) {
	command := agents.Command{Path: "/claude", Args: []string{"--print", "review"}}
	if err := Configure(&command, "dari_route", "https://routing.dari.dev", agents.Models{Routing: true}); err != nil {
		t.Fatal(err)
	}
	if command.Cleanup == nil {
		t.Fatal("Claude command has no cleanup")
	}

	settingsIndex := slices.Index(command.Args, "--settings")
	if settingsIndex < 0 || settingsIndex+1 >= len(command.Args) {
		t.Fatalf("args do not contain --settings: %q", command.Args)
	}
	settingsPath := command.Args[settingsIndex+1]
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	var settings struct {
		StatusLine struct {
			Type            string `json:"type"`
			Command         string `json:"command"`
			RefreshInterval int    `json:"refreshInterval"`
		} `json:"statusLine"`
		ModelPicker struct {
			Options               []map[string]string `json:"options"`
			ReplaceBuiltInOptions bool                `json:"replaceBuiltInOptions"`
		} `json:"modelPicker"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ANTHROPIC_DEFAULT_OPUS_MODEL", "ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_DEFAULT_FABLE_MODEL"} {
		if got := agents.EnvironmentValue(command.Env, name); got != agents.RoutingModel {
			t.Errorf("%s = %q, want the launch's model", name, got)
		}
	}
	// The picker lists only the router's models, in place of Claude's own.
	if !settings.ModelPicker.ReplaceBuiltInOptions || len(settings.ModelPicker.Options) == 0 {
		t.Fatalf("modelPicker = %+v; want the router's models replacing the built-in lineup", settings.ModelPicker)
	}
	if settings.StatusLine.Type != "command" {
		t.Fatalf("statusLine.type = %q", settings.StatusLine.Type)
	}
	if !strings.Contains(settings.StatusLine.Command, " agent-status --claude") {
		t.Fatalf("status command = %q", settings.StatusLine.Command)
	}
	if settings.StatusLine.RefreshInterval <= 0 || settings.StatusLine.RefreshInterval*int(time.Second) >= int(agents.CacheTTL) {
		t.Fatalf("status refresh = %d", settings.StatusLine.RefreshInterval)
	}
	if strings.Contains(string(data), "dari_route") {
		t.Fatal("Claude status settings contain the secret key")
	}

	command.Cleanup()
	if _, err := os.Stat(settingsPath); !os.IsNotExist(err) {
		t.Fatalf("temporary settings still exist: %v", err)
	}
}

func TestBuildClaudeAgentCommandSkipsStatusLineWhenSettingsFlagPresent(t *testing.T) {
	command := agents.Command{Path: "/claude", Args: []string{"--settings", "mine.json"}}
	if err := Configure(&command, "dari_route", "https://routing.dari.dev", agents.Models{Routing: true}); err != nil {
		t.Fatal(err)
	}
	if command.Cleanup != nil {
		t.Fatal("Claude command should not own cleanup when --settings is already set")
	}
	if slices.Index(command.Args, "--settings") != 0 || command.Args[1] != "mine.json" {
		t.Fatalf("args = %q", command.Args)
	}
}

func TestClaudeStatusLineReadsDariRouting(t *testing.T) {
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	assistant := func(model string, cacheRead any, respondedAt time.Time) map[string]any {
		message := map[string]any{
			"model": model,
			"dari_routing": map[string]any{
				"selected_model":        model,
				"reasoning_effort":      "high",
				"lease_turns_remaining": 1,
			},
		}
		if cacheRead != nil {
			message["usage"] = map[string]any{"input_tokens": 12, "cache_read_input_tokens": cacheRead}
		}
		return map[string]any{"type": "assistant", "timestamp": respondedAt.Format(time.RFC3339Nano), "message": message}
	}
	writeTranscript := func(lines ...map[string]any) {
		t.Helper()
		var body strings.Builder
		for _, line := range append([]map[string]any{{"type": "user", "message": map[string]any{"role": "user"}}}, lines...) {
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
	}
	render := func(stdin map[string]any) string {
		t.Helper()
		encoded, err := json.Marshal(stdin)
		if err != nil {
			t.Fatal(err)
		}
		status, err := TranscriptStatus(strings.NewReader(string(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		return status.String()
	}
	stdin := map[string]any{"transcript_path": transcript}
	now := time.Now()
	for _, tc := range []struct {
		lines []map[string]any
		want  string
	}{
		{[]map[string]any{assistant("xai/grok-4.6", nil, now)}, "xai/grok-4.6 · high · lease 2 turns left · cache cold"},
		{[]map[string]any{assistant("xai/grok-4.6", 4096, now)}, "xai/grok-4.6 · high · lease 2 turns left · cache warm"},
		{[]map[string]any{assistant("xai/grok-4.6", 0, now)}, "xai/grok-4.6 · high · lease 2 turns left · cache cold"},
		{[]map[string]any{assistant("xai/grok-4.6", 4096, now.Add(-agents.CacheTTL-time.Second))}, "xai/grok-4.6 · high · lease 2 turns left · cache cold"},
		// A new model's first entry carries its own usage, not the previous turn's.
		{[]map[string]any{assistant("xai/grok-4.6", 4096, now), assistant("openai/gpt-5.5", 0, now)}, "openai/gpt-5.5 · high · lease 2 turns left · cache cold"},
		// A switched model's in-progress entry has no cache usage yet; the
		// previous model's warm usage must not carry over.
		{[]map[string]any{assistant("xai/grok-4.6", 4096, now), assistant("openai/gpt-5.5", nil, now)}, "openai/gpt-5.5 · high · lease 2 turns left · cache cold"},
	} {
		writeTranscript(tc.lines...)
		if got := render(stdin); got != tc.want {
			t.Errorf("status = %q, want %q", got, tc.want)
		}
	}
}
