package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func configureClaudeCommand(command *agentCommand, key, routerID string) error {
	command.env = withEnvironment(command.env, map[string]string{
		"ANTHROPIC_BASE_URL":               agentRoutingBaseURL(routerID),
		"ANTHROPIC_AUTH_TOKEN":             key,
		"ANTHROPIC_API_KEY":                "",
		"CLAUDE_CODE_EFFORT_LEVEL":         "auto",
		"CLAUDE_CODE_GATEWAY_HINT_HEADERS": "1",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS":   "1000000",
		"CLAUDE_CODE_SUBAGENT_MODEL":       routingModel,
	})
	if !argsContainFlag(command.args, "--settings") {
		if err := injectClaudeStatusLine(command); err != nil {
			return err
		}
	}
	command.args = insertBeforeDoubleDash(command.args, []string{"--model", routingModel})
	return nil
}

func injectClaudeStatusLine(command *agentCommand) error {
	executable, err := agentStatusExecutable()
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
			"command": shellQuote(executable) + " agent-status --claude",
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
	command.cleanup = func() { _ = os.Remove(path) }
	command.args = insertBeforeDoubleDash(command.args, []string{"--settings", path})
	return nil
}

func argsContainFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}
	return false
}
