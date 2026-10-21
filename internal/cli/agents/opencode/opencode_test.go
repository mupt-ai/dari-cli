package opencode

import (
	"encoding/json"
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildOpenCodeCommandInjectsSessionConfig(t *testing.T) {
	command := launch()
	if err := Configure(&command, "https://routing.dari.dev/router_1", agents.Models{Routing: true}); err != nil {
		t.Fatal(err)
	}
	if command.Cleanup == nil {
		t.Fatal("OpenCode command has no cleanup")
	}
	if !slices.Equal(command.Args, []string{"--standalone"}) {
		t.Fatalf("args = %q", command.Args)
	}
	raw := agents.EnvironmentValue(command.Env, "OPENCODE_CONFIG_CONTENT")
	if strings.Contains(raw, "dari_route") {
		t.Fatal("config contains the secret key")
	}
	var config struct {
		Model            string                       `json:"model"`
		SmallModel       string                       `json:"small_model"`
		EnabledProviders []string                     `json:"enabled_providers"`
		Agent            map[string]map[string]string `json:"agent"`
		Plugins          []string                     `json:"plugins"`
		Provider         map[string]struct {
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
	if !slices.Equal(config.EnabledProviders, []string{"dari"}) {
		t.Errorf("enabled_providers = %q, want only dari in /models", config.EnabledProviders)
	}
	if config.Model != agents.RoutingModel || config.SmallModel != agents.RoutingModel {
		t.Errorf("model = %q, small_model = %q", config.Model, config.SmallModel)
	}
	for _, name := range []string{"general", "explore", "scout", "title", "summary", "compaction"} {
		if config.Agent[name]["model"] != agents.RoutingModel {
			t.Errorf("agent %s model = %q", name, config.Agent[name]["model"])
		}
	}
	// Primary agents follow /models.
	for _, name := range []string{"build", "plan"} {
		if _, pinned := config.Agent[name]; pinned {
			t.Errorf("agent %s is pinned", name)
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
	if model.ID != agents.RoutingModel {
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
	for _, want := range []string{`ctx.session.hook("model.request"`, `"x-opencode-request-kind"`, `"x-opencode-agent"`, `ctx.session.hook("http.response"`, `"x-dari-selected-model"`, `"agent-status", "--headers"`, `providerID: "dari"`,
		`"--cached-tokens", String(route.cachedTokens)`, `const copy = event.response.clone()`, `, ...args], { stdout: "pipe"`,
		`expiries[event.sessionID] = setTimeout(() => render(event.sessionID).catch(() => {}), cacheTTL)`,
		`process.env.DARI_STATUS_EXECUTABLE`, `process.env.DARI_CACHE_TTL_MS`,
		`generations[sessionID] !== generation`, `clearTimeout(expiries[event.sessionID])`,
	} {
		if !strings.Contains(string(server), want) {
			t.Errorf("server plugin does not record routing status (%s missing)", want)
		}
	}
	if string(server) != serverPlugin {
		t.Error("server plugin differs from agents/opencode/server.ts")
	}
	if agents.EnvironmentValue(command.Env, "DARI_STATUS_EXECUTABLE") == "" || agents.EnvironmentValue(command.Env, "DARI_CACHE_TTL_MS") != "300000" {
		t.Errorf("status environment = %q, %q", agents.EnvironmentValue(command.Env, "DARI_STATUS_EXECUTABLE"), agents.EnvironmentValue(command.Env, "DARI_CACHE_TTL_MS"))
	}
	tui, err := os.ReadFile(filepath.Join(plugin, "tui.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tui), `append: "prompt.footer.status"`) {
		t.Errorf("TUI plugin does not show routing status: %s", tui)
	}

	command.Cleanup()
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
		{"run replaces user model", []string{"run", "-m", "openai/gpt", "fix it"}, []string{"run", "fix it", "--standalone", "--model", agents.RoutingModel}},
		{"run keeps prompt after double dash", []string{"run", "--", "--help"}, []string{"run", "--standalone", "--model", agents.RoutingModel, "--", "--help"}},
		{"mini replaces user model", []string{"mini", "--model=openai/gpt"}, []string{"mini", "--standalone", "--model", agents.RoutingModel}},
		{"explicit standalone", []string{"--standalone", "--auto"}, []string{"--standalone", "--auto"}},
		{"mini", []string{"mini"}, []string{"mini", "--standalone", "--model", agents.RoutingModel}},
		{"models command does not get model flag", []string{"models"}, []string{"models", "--standalone"}},
		{"stats command does not get model flag", []string{"stats"}, []string{"stats", "--standalone"}},
		{"api uses private server without model flag", []string{"api", "GET", "/session"}, []string{"api", "GET", "/session", "--standalone"}},
		{"leading flag before models", []string{"--print-logs", "models"}, []string{"--print-logs", "models", "--standalone"}},
		{"leading valued flag before serve", []string{"--log-level", "debug", "serve"}, []string{"--log-level", "debug", "serve"}},
		{"leading valued flag before run", []string{"--log-level", "debug", "run", "hi"}, []string{"--log-level", "debug", "run", "hi", "--standalone", "--model", agents.RoutingModel}},
		{"interactive session value is not a subcommand", []string{"-s", "ses_1"}, []string{"-s", "ses_1", "--standalone"}},
		{"serve inherits environment", []string{"serve"}, []string{"serve"}},
		{"help", []string{"--help"}, []string{"--help"}},
		{"version", []string{"run", "-v"}, []string{"run", "-v"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := launch(tt.args...)
			if err := Configure(&command, "https://routing.dari.dev", agents.Models{Routing: true}); err != nil {
				t.Fatal(err)
			}
			defer command.Cleanup()
			if !slices.Equal(command.Args, tt.want) {
				t.Fatalf("args = %q, want %q", command.Args, tt.want)
			}
			if agents.EnvironmentValue(command.Env, "OPENCODE_CONFIG_CONTENT") == "" {
				t.Fatal("OpenCode config was not injected")
			}
		})
	}
}

func TestBuildOpenCodeManagementCommandsGetNoDariConfig(t *testing.T) {
	for _, args := range [][]string{{"service", "start"}, {"session", "list"}, {"--print-logs", "service", "restart"}} {
		command := launch(args...)
		if err := Configure(&command, "https://routing.dari.dev", agents.Models{Routing: true}); err != nil {
			t.Fatal(err)
		}
		if command.Cleanup != nil {
			t.Errorf("%q wrote a temporary plugin", args)
		}
		if !slices.Equal(command.Args, args) {
			t.Errorf("args = %q, want %q", command.Args, args)
		}
		for _, name := range []string{"OPENCODE_CONFIG_CONTENT", agents.RoutingAPIKeyEnv} {
			if agents.EnvironmentValue(command.Env, name) != "" {
				t.Errorf("%q passes %s to the shared service", args, name)
			}
		}
	}
}

func TestBuildOpenCodeCommandRejectsPairing(t *testing.T) {
	command := launch("--print-logs", "pair")
	err := Configure(&command, "https://routing.dari.dev", agents.Models{Routing: true})
	if err == nil || !strings.Contains(err.Error(), "dari opencode serve") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildOpenCodeCommandRejectsSharedServer(t *testing.T) {
	command := launch("--server", "http://127.0.0.1:4096")
	err := Configure(&command, "https://routing.dari.dev", agents.Models{Routing: true})
	if err == nil || !strings.Contains(err.Error(), "remove --server") {
		t.Fatalf("error = %v", err)
	}
}

// A TTL render that captured an older cache reading must not overwrite the
// newer warm status written when the next response's usage arrives.
func TestOpenCodeStatusPluginDropsStaleRenders(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	dir := t.TempDir()
	// Expired readings render slowly, so the TTL render finishes last.
	script := `const i = process.argv.indexOf("--responded-at")
const age = i < 0 ? Infinity : Date.now() - Number(process.argv[i + 1])
if (age >= 300) { await Bun.sleep(800); console.log("m · cache cold") } else console.log("m · cache warm")
`
	source := serverPlugin
	if err := os.MkdirAll(filepath.Join(dir, "plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin", "server.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import plugin from "./plugin/server.ts";
import { readFileSync } from "node:fs";
let hook: Function = () => {};
await plugin.setup({ session: { hook: async (_: string, fn: Function) => { hook = fn; } } });
const usage = 'data: {"usage":{"prompt_tokens_details":{"cached_tokens":8}}}\n';
const headers = { "x-dari-selected-model": "m" };
await hook({ kind: "primary", sessionID: "s", response: new Response(usage, { headers }) });
await Bun.sleep(150);
let finish: Function = () => {};
const body = new ReadableStream({ start(c) { finish = () => { c.enqueue(new TextEncoder().encode(usage)); c.close() } } });
await hook({ kind: "primary", sessionID: "s", response: new Response(body, { headers }) });
await Bun.sleep(300); // The first TTL render has started on the old reading.
finish();
await Bun.sleep(900); // The stale render has finished; the new TTL has not.
console.log(JSON.parse(readFileSync("./status.json", "utf8")).s);
process.exit(0);
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := statusPluginCommand(t, bun, dir, script, "300")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "m · cache warm" {
		t.Fatalf("status = %q", got)
	}
}

// A header render that computed "cache warm" just before expiry and finishes
// after the TTL re-render must not leave the footer warm.
func TestOpenCodeStatusPluginExpiryBeatsInFlightWarmRender(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	dir := t.TempDir()
	script := `const i = process.argv.indexOf("--responded-at")
const age = i < 0 ? Infinity : Date.now() - Number(process.argv[i + 1])
if (age < 300) { await Bun.sleep(400); console.log("m · cache warm") } else console.log("m · cache cold")
`
	source := serverPlugin
	if err := os.MkdirAll(filepath.Join(dir, "plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin", "server.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import plugin from "./plugin/server.ts";
import { readFileSync } from "node:fs";
let hook: Function = () => {};
await plugin.setup({ session: { hook: async (_: string, fn: Function) => { hook = fn; } } });
const usage = 'data: {"usage":{"prompt_tokens_details":{"cached_tokens":8}}}\n';
const headers = { "x-dari-selected-model": "m" };
await hook({ kind: "primary", sessionID: "s", response: new Response(usage, { headers }) });
await Bun.sleep(100); // The usage render is in flight; the TTL timer is armed.
// Same model, body never finishes: this render copies the warm reading.
hook({ kind: "primary", sessionID: "s", response: new Response(new ReadableStream({}), { headers }) });
await Bun.sleep(1200);
console.log(JSON.parse(readFileSync("./status.json", "utf8")).s);
process.exit(0);
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := statusPluginCommand(t, bun, dir, script, "300")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "m · cache cold" {
		t.Fatalf("status = %q", got)
	}
}

// The client may consume the body as soon as the hook returns; the usage copy
// must already exist so cache warmth still updates.
func TestOpenCodeStatusPluginClonesBeforeClientReads(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	dir := t.TempDir()
	script := `await Bun.sleep(100)
console.log(process.argv.includes("--cached-tokens") ? "m · cache warm" : "m · cache cold")
`
	source := serverPlugin
	if err := os.MkdirAll(filepath.Join(dir, "plugin"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin", "server.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import plugin from "./plugin/server.ts";
import { readFileSync } from "node:fs";
let hook: Function = () => {};
await plugin.setup({ session: { hook: async (_: string, fn: Function) => { hook = fn; } } });
const response = new Response('data: {"usage":{"prompt_tokens_details":{"cached_tokens":8}}}\n', { headers: { "x-dari-selected-model": "m" } });
const pending = hook({ kind: "primary", sessionID: "s", response });
await response.text(); // The client reads the whole body right away.
await pending;
await Bun.sleep(800);
console.log(JSON.parse(readFileSync("./status.json", "utf8")).s);
process.exit(0);
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := statusPluginCommand(t, bun, dir, script, "60000")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "m · cache warm" {
		t.Fatalf("status = %q", got)
	}
}

// statusPluginCommand runs a bun harness in dir with the environment an agent
// launch sets, using renderScript as the status renderer.
func statusPluginCommand(t *testing.T, bun, dir, renderScript, ttl string) *exec.Cmd {
	t.Helper()
	script := filepath.Join(dir, "render.ts")
	renderer := filepath.Join(dir, "render.sh")
	if err := os.WriteFile(script, []byte(renderScript), 0o600); err != nil {
		t.Fatal(err)
	}
	wrapper := "#!/bin/sh\nexec " + agents.ShellQuote(bun) + " " + agents.ShellQuote(script) + " \"$@\"\n"
	if err := os.WriteFile(renderer, []byte(wrapper), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bun, "run.ts")
	cmd.Dir = dir
	cmd.Env = agents.WithEnvironment(os.Environ(), map[string]string{"DARI_STATUS_EXECUTABLE": renderer, "DARI_CACHE_TTL_MS": ttl})
	return cmd
}

// launch builds the command the launcher passes to Configure: OpenCode's
// arguments with the routing key in the environment.
func launch(args ...string) agents.Command {
	return agents.Command{Path: "/opencode", Args: args, Env: []string{agents.RoutingAPIKeyEnv + "=dari_route"}}
}
