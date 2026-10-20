package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

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

func configureOpenCodeCommand(command *agentCommand, routerID string) error {
	info := onlyRequestsAgentInfo(command.args)
	subcommand := openCodeSubcommand(command.args)
	if !info && argsContainFlag(command.args, "--server") {
		return errors.New("dari opencode starts a private OpenCode server; remove --server")
	}
	// Pairing always targets the shared background service, which Dari cannot route.
	if !info && subcommand == "pair" {
		return errors.New("dari opencode cannot route paired clients; run `dari opencode serve` and connect to it instead")
	}
	if openCodeManagementCommands[subcommand] {
		command.env = withoutEnvironment(command.env, routingAPIKeyEnv)
		return nil
	}
	directory, err := writeOpenCodeStatusPlugin()
	if err != nil {
		return err
	}
	command.cleanup = func() { _ = os.RemoveAll(directory) }
	config, err := openCodeConfig(agentRoutingBaseURL(routerID), filepath.Join(directory, "plugin"))
	if err != nil {
		command.cleanup()
		return err
	}
	command.env = withEnvironment(command.env, map[string]string{"OPENCODE_CONFIG_CONTENT": config})
	if info {
		return nil
	}
	if !openCodeInProcessServers[subcommand] && !argsContainFlag(command.args, "--standalone") {
		command.args = insertBeforeDoubleDash(command.args, []string{"--standalone"})
	}
	if openCodeModelCommands[subcommand] {
		command.args = replaceFlag(command.args, "--model", []string{"--model", routingModel})
	}
	return nil
}

func withoutEnvironment(env []string, name string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, name+"=") {
			result = append(result, entry)
		}
	}
	return result
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
	return insertBeforeDoubleDash(result, value)
}

func openCodeConfig(baseURL, pluginDirectory string) (string, error) {
	agents := make(map[string]any, len(openCodeAgents))
	for _, name := range openCodeAgents {
		agents[name] = map[string]string{"model": routingModel}
	}
	body, err := json.Marshal(map[string]any{
		"model":       routingModel,
		"small_model": routingModel,
		"agent":       agents,
		"plugins":     []string{pluginDirectory},
		"provider": map[string]any{
			"dari": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Dari",
				"env":  []string{routingAPIKeyEnv},
				"options": map[string]string{
					"baseURL": baseURL + "/v1",
					"apiKey":  "{env:" + routingAPIKeyEnv + "}",
				},
				"models": map[string]any{
					"routing": map[string]any{
						"id":         routingModel,
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

// The server half tags each request with its kind and agent, reads each Dari
// response's routing headers, and writes the rendered status per session next
// to the plugin; the TUI half shows the prompt's session status in its footer. The file lives outside the plugin
// directory so status writes do not trigger OpenCode's plugin reloads.
const openCodeStatusServerPlugin = `import { renameSync, writeFileSync } from "node:fs"

const statusFile = new URL("../status.json", import.meta.url)
const headerNames = ["x-dari-selected-model", "x-dari-thinking-level", "x-dari-lease-turns-remaining"]

export default {
  id: "dari.status",
  setup: async (ctx: any) => {
    // OpenCode names the session but not the kind of request or the agent;
    // Dari uses both to split Activity into main, subagent, and background work.
    await ctx.session.hook("model.request", (event: any) => {
      event.headers["x-opencode-request-kind"] = event.kind
      event.headers["x-opencode-agent"] = event.agent
    }, { providerID: "dari" })
    const statuses: Record<string, string> = {}
    await ctx.session.hook("http.response", async (event: any) => {
      const response = event.response.headers
      if (event.kind !== "primary" || !response.get(headerNames[0])) return
      const headers = Object.fromEntries(headerNames.filter(name => response.has(name)).map(name => [name, response.get(name)]))
      const child = Bun.spawn([DARI_STATUS_EXECUTABLE, "agent-status", "--headers", JSON.stringify(headers)], { stdout: "pipe", stderr: "ignore" })
      const status = (await new Response(child.stdout).text()).trim()
      if ((await child.exited) !== 0 || !status) return
      statuses[event.sessionID] = status
      const temporary = new URL("../status.json.tmp", import.meta.url)
      writeFileSync(temporary, JSON.stringify(statuses))
      renameSync(temporary, statusFile)
    }, { providerID: "dari" })
  },
}
`

const openCodeStatusTUIPlugin = `import { readFileSync } from "node:fs"
import { createSignal, Show } from "solid-js"

const statusFile = new URL("../status.json", import.meta.url)

export default {
  id: "dari.status",
  setup(context: any) {
    const [statuses, setStatuses] = createSignal<Record<string, string>>({})
    let last = ""
    const timer = setInterval(() => {
      try {
        const content = readFileSync(statusFile, "utf8")
        if (content !== last) setStatuses(JSON.parse((last = content)))
      } catch {}
    }, 500)
    context.ui.slot({
      append: "prompt.footer.status",
      render: (input: { sessionID?: string }) => (
        <Show when={input.sessionID && statuses()[input.sessionID]}>
          <text fg={context.theme.text.muted}>{statuses()[input.sessionID!]}</text>
        </Show>
      ),
    })
    return () => clearInterval(timer)
  },
}
`

func writeOpenCodeStatusPlugin() (directory string, err error) {
	executable, err := agentStatusExecutable()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(executable)
	if err != nil {
		return "", err
	}
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
		"server.ts": strings.ReplaceAll(openCodeStatusServerPlugin, "DARI_STATUS_EXECUTABLE", string(encoded)),
		"tui.tsx":   openCodeStatusTUIPlugin,
	}
	for name, content := range files {
		if err = os.WriteFile(filepath.Join(plugin, name), []byte(content), 0o600); err != nil {
			return "", fmt.Errorf("write OpenCode plugin: %w", err)
		}
	}
	return directory, nil
}
