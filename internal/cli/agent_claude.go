package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func configureClaudeCommand(command *agentCommand, key, routerID string) error {
	command.env = withEnvironment(command.env, map[string]string{
		"ANTHROPIC_BASE_URL":             agentRoutingBaseURL(routerID),
		"ANTHROPIC_AUTH_TOKEN":           key,
		"ANTHROPIC_API_KEY":              "",
		"CLAUDE_CODE_EFFORT_LEVEL":       "auto",
		"CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",
		"CLAUDE_CODE_SUBAGENT_MODEL":     routingModel,
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
	scriptPath, settingsPath, err := writeClaudeStatusLine()
	if err != nil {
		return err
	}
	command.cleanup = func() {
		_ = os.Remove(scriptPath)
		_ = os.Remove(settingsPath)
	}
	command.args = insertBeforeDoubleDash(command.args, []string{"--settings", settingsPath})
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

// Claude Code has no Pi-style setStatus hook. The launcher injects a
// session-scoped statusLine that reads dari_routing off the transcript:
// serving model, thinking level, and turns left on the current lease.
const claudeStatusLineScript = `#!/usr/bin/env python3
import json, sys

def last_routing(path):
    last = None
    try:
        with open(path, encoding="utf-8") as f:
            for line in f:
                try:
                    obj = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if obj.get("type") != "assistant":
                    continue
                routing = (obj.get("message") or {}).get("dari_routing")
                if isinstance(routing, dict) and routing.get("selected_model"):
                    last = routing
    except OSError:
        return None
    return last

try:
    payload = json.load(sys.stdin)
except (json.JSONDecodeError, OSError):
    sys.exit(0)

routing = last_routing(payload.get("transcript_path") or "")
if not routing:
    sys.exit(0)

model = routing.get("selected_model")
if not isinstance(model, str) or not model:
    sys.exit(0)

parts = [model]
thinking = routing.get("reasoning_effort")
if thinking and thinking != "off":
    parts.append(str(thinking))
remaining = routing.get("lease_turns_remaining")
if remaining is not None:
    try:
        turns = int(remaining) + 1
    except (TypeError, ValueError):
        turns = None
    if turns is not None:
        unit = "turn" if turns == 1 else "turns"
        parts.append("lease %s %s left" % (turns, unit))
print(" · ".join(parts))
`

func writeClaudeStatusLine() (scriptPath, settingsPath string, err error) {
	script, err := os.CreateTemp("", "dari-claude-status-*.py")
	if err != nil {
		return "", "", fmt.Errorf("create temporary Claude status line: %w", err)
	}
	scriptPath = script.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(scriptPath)
			if settingsPath != "" {
				_ = os.Remove(settingsPath)
			}
		}
	}()
	if err = script.Chmod(0o700); err != nil {
		_ = script.Close()
		return "", "", fmt.Errorf("set Claude status line permissions: %w", err)
	}
	if _, err = script.WriteString(claudeStatusLineScript); err != nil {
		_ = script.Close()
		return "", "", fmt.Errorf("write Claude status line: %w", err)
	}
	if err = script.Close(); err != nil {
		return "", "", fmt.Errorf("close Claude status line: %w", err)
	}

	settings, err := os.CreateTemp("", "dari-claude-settings-*.json")
	if err != nil {
		return "", "", fmt.Errorf("create temporary Claude settings: %w", err)
	}
	settingsPath = settings.Name()
	body, err := json.Marshal(map[string]any{
		"statusLine": map[string]any{
			"type":    "command",
			"command": scriptPath,
		},
	})
	if err != nil {
		_ = settings.Close()
		return "", "", fmt.Errorf("encode Claude settings: %w", err)
	}
	if err = settings.Chmod(0o600); err != nil {
		_ = settings.Close()
		return "", "", fmt.Errorf("set Claude settings permissions: %w", err)
	}
	if _, err = settings.Write(body); err != nil {
		_ = settings.Close()
		return "", "", fmt.Errorf("write Claude settings: %w", err)
	}
	if err = settings.Close(); err != nil {
		return "", "", fmt.Errorf("close Claude settings: %w", err)
	}
	return scriptPath, settingsPath, nil
}
