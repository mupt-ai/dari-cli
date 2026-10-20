// Package claude launches Claude Code through Dari and renders its status
// line from the session transcript.
package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

// Configure points Claude Code at the router at baseURL and, unless the user
// passed their own --settings, adds Dari's status line.
func Configure(command *agents.Command, key, baseURL string) error {
	command.Env = agents.WithEnvironment(command.Env, map[string]string{
		"ANTHROPIC_BASE_URL":               baseURL,
		"ANTHROPIC_AUTH_TOKEN":             key,
		"ANTHROPIC_API_KEY":                "",
		"CLAUDE_CODE_EFFORT_LEVEL":         "auto",
		"CLAUDE_CODE_GATEWAY_HINT_HEADERS": "1",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":   "1000000",
		"CLAUDE_CODE_SUBAGENT_MODEL":       agents.RoutingModel,
	})
	if !agents.ArgsContainFlag(command.Args, "--settings") {
		if err := injectStatusLine(command); err != nil {
			return err
		}
	}
	command.Args = agents.InsertBeforeDoubleDash(command.Args, []string{"--model", agents.RoutingModel})
	return nil
}

func injectStatusLine(command *agents.Command) error {
	executable, err := agents.StatusExecutable()
	if err != nil {
		return err
	}
	settings, err := os.CreateTemp("", "dari-claude-settings-*.json")
	if err != nil {
		return fmt.Errorf("create temporary Claude settings: %w", err)
	}
	path := settings.Name()
	body, err := json.Marshal(map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": agents.ShellQuote(executable) + " agent-status --claude",
			// Idle sessions must flip to "cache cold" once the cache TTL passes.
			"refreshInterval": 30,
		},
	})
	if err == nil {
		_, err = settings.Write(body)
	}
	closeErr := settings.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("write Claude status settings: %w", err)
	}
	command.Cleanup = func() { _ = os.Remove(path) }
	command.Args = agents.InsertBeforeDoubleDash(command.Args, []string{"--settings", path})
	return nil
}

// TranscriptStatus renders the status line from the status-line input Claude
// Code passes on stdin. The transcript keeps routing metadata and usage on
// each assistant entry, so cache state and its response timestamp stay
// aligned to one turn.
func TranscriptStatus(input io.Reader) (agents.Status, error) {
	var payload struct {
		TranscriptPath string `json:"transcript_path"`
	}
	if err := json.NewDecoder(io.LimitReader(input, 1<<20)).Decode(&payload); err != nil {
		return agents.Status{}, err
	}
	file, err := os.Open(payload.TranscriptPath)
	if err != nil {
		return agents.Status{}, err
	}
	defer file.Close()
	var last agents.Status
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	for scanner.Scan() {
		var entry struct {
			Type      string    `json:"type"`
			Timestamp time.Time `json:"timestamp"`
			Message   struct {
				Routing agents.Status `json:"dari_routing"`
				Usage   struct {
					CacheReadInputTokens *int64 `json:"cache_read_input_tokens"`
				} `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(scanner.Bytes(), &entry) == nil && entry.Type == "assistant" && entry.Message.Routing.Model != "" {
			last = entry.Message.Routing
			last.CachedTokens = entry.Message.Usage.CacheReadInputTokens
			last.RespondedAt = entry.Timestamp
		}
	}
	return last, scanner.Err()
}
