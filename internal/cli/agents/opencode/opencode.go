// Package opencode launches OpenCode with session-local Dari configuration
// and a status plugin. The plugin's server and TUI halves are server.ts and
// tui.tsx, embedded as written.
package opencode

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

//go:embed server.ts
var serverPlugin string

//go:embed tui.tsx
var tuiPlugin string

// OpenCode subcommands that manage local state or the shared background
// service. That service can outlive this launch, so they get none of Dari's
// session-local configuration, temporary plugin, or routing key.
var openCodeManagementCommands = map[string]bool{
	"auth": true, "debug": true, "mcp": true, "plugin": true, "reload": true,
	"service": true, "session": true, "uninstall": true, "update": true, "upgrade": true,
}

// OpenCode subcommands that run their own in-process server, which already
// inherits Dari's environment. Every other invocation connects to the shared
// background service unless it is given --standalone.
var openCodeInProcessServers = map[string]bool{"acp": true, "serve": true}

// openCodeModelCommands accept --model. The interactive TUI does not; it uses
// the configured model instead, which outranks remembered selections.
var openCodeModelCommands = map[string]bool{"mini": true, "run": true}

// openCodeAgents are OpenCode's built-in primary agents, subagents, and
// small-model tasks. Pinning each keeps an agent model set in the user's own
// configuration from bypassing the router.
var openCodeAgents = []string{"build", "plan", "general", "explore", "scout", "title", "summary", "compaction"}

// Configure points OpenCode at the router at baseURL through session-local
// configuration and adds the status plugin.
func Configure(command *agents.Command, baseURL string) error {
	info := agents.OnlyRequestsInfo(command.Args)
	subcommand := openCodeSubcommand(command.Args)
	if !info && agents.ArgsContainFlag(command.Args, "--server") {
		return errors.New("dari opencode starts a private OpenCode server; remove --server")
	}
	// Pairing always targets the shared background service, which Dari cannot route.
	if !info && subcommand == "pair" {
		return errors.New("dari opencode cannot route paired clients; run `dari opencode serve` and connect to it instead")
	}
	if openCodeManagementCommands[subcommand] {
		command.Env = agents.WithoutEnvironment(command.Env, agents.RoutingAPIKeyEnv)
		return nil
	}
	status, err := agents.StatusEnvironment()
	if err != nil {
		return err
	}
	directory, err := writeOpenCodeStatusPlugin()
	if err != nil {
		return err
	}
	command.Cleanup = func() { _ = os.RemoveAll(directory) }
	config, err := openCodeConfig(baseURL, filepath.Join(directory, "plugin"))
	if err != nil {
		command.Cleanup()
		return err
	}
	status["OPENCODE_CONFIG_CONTENT"] = config
	command.Env = agents.WithEnvironment(command.Env, status)
	if info {
		return nil
	}
	if !openCodeInProcessServers[subcommand] && !agents.ArgsContainFlag(command.Args, "--standalone") {
		command.Args = agents.InsertBeforeDoubleDash(command.Args, []string{"--standalone"})
	}
	if openCodeModelCommands[subcommand] {
		command.Args = replaceFlag(command.Args, "--model", []string{"--model", agents.RoutingModel})
	}
	return nil
}

// openCodeValueFlags are OpenCode flags whose value is a separate argument, so
// it is not mistaken for the subcommand.
var openCodeValueFlags = map[string]bool{
	"--agent": true, "--completions": true, "--log-level": true, "--model": true, "-m": true,
	"--prompt": true, "--server": true, "--session": true, "-s": true,
}

// openCodeSubcommand returns the first positional argument, skipping leading
// flags such as `--print-logs` that OpenCode accepts before the subcommand.
func openCodeSubcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return ""
		}
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
		if openCodeValueFlags[arg] {
			i++
		}
	}
	return ""
}

func replaceFlag(args []string, flag string, value []string) []string {
	result := make([]string, 0, len(args)+len(value))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			result = append(result, args[i:]...)
			break
		}
		if arg == flag || flag == "--model" && arg == "-m" {
			i++
			continue
		}
		if strings.HasPrefix(arg, flag+"=") || flag == "--model" && strings.HasPrefix(arg, "-m=") {
			continue
		}
		result = append(result, arg)
	}
	return agents.InsertBeforeDoubleDash(result, value)
}

func openCodeConfig(baseURL, pluginDirectory string) (string, error) {
	pinned := make(map[string]any, len(openCodeAgents))
	for _, name := range openCodeAgents {
		pinned[name] = map[string]string{"model": agents.RoutingModel}
	}
	body, err := json.Marshal(map[string]any{
		"model":       agents.RoutingModel,
		"small_model": agents.RoutingModel,
		"agent":       pinned,
		"plugins":     []string{pluginDirectory},
		"provider": map[string]any{
			"dari": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Dari",
				"env":  []string{agents.RoutingAPIKeyEnv},
				"options": map[string]string{
					"baseURL": baseURL + "/v1",
					"apiKey":  "{env:" + agents.RoutingAPIKeyEnv + "}",
				},
				"models": map[string]any{
					"routing": map[string]any{
						"id":         agents.RoutingModel,
						"name":       "Dari Router",
						"attachment": true,
						"reasoning":  false,
						"tool_call":  true,
						"limit":      map[string]int{"context": 1_000_000, "output": 128_000},
						"modalities": map[string][]string{"input": {"text", "image"}, "output": {"text"}},
					},
				},
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("encode OpenCode config: %w", err)
	}
	return string(body), nil
}

func writeOpenCodeStatusPlugin() (directory string, err error) {
	directory, err = os.MkdirTemp("", "dari-opencode-*")
	if err != nil {
		return "", fmt.Errorf("create temporary OpenCode plugin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(directory)
		}
	}()
	plugin := filepath.Join(directory, "plugin")
	if err = os.Mkdir(plugin, 0o700); err != nil {
		return "", fmt.Errorf("create temporary OpenCode plugin: %w", err)
	}
	files := map[string]string{
		"server.ts": serverPlugin,
		"tui.tsx":   tuiPlugin,
	}
	for name, content := range files {
		if err = os.WriteFile(filepath.Join(plugin, name), []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write OpenCode plugin: %w", err)
		}
	}
	return directory, nil
}
