package pi

import (
	"github.com/mupt-ai/dari-cli/internal/cli/agents"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestBuildPiAgentCommandUsesTemporaryProviderExtension(t *testing.T) {
	command := agents.Command{Path: "/pi", Args: []string{"-p", "review"}, Env: []string{agents.RoutingAPIKeyEnv + "=dari_route"}}
	if err := Configure(&command, agents.Models{Routing: true}); err != nil {
		t.Fatal(err)
	}
	if command.Cleanup == nil {
		t.Fatal("Pi command has no cleanup")
	}

	extensionIndex := slices.Index(command.Args, "--extension")
	if extensionIndex < 0 || extensionIndex+1 >= len(command.Args) {
		t.Fatalf("args do not contain an extension: %q", command.Args)
	}
	path := command.Args[extensionIndex+1]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `pi.registerProvider("dari"`) {
		t.Errorf("extension does not register Dari: %s", content)
	}
	for _, want := range []string{
		`pi.on("after_provider_response"`,
		`x-dari-selected-model`,
		`"agent-status", "--headers"`,
		`ctx.ui.setStatus("dari"`,
		`pi.on("message_end"`,
		`"--cached-tokens", String(message.usage.cacheRead ?? 0)`,
		`if (cache === currentCache) render(ctx, ++revision).catch(() => {})`,
		`process.env.DARI_STATUS_EXECUTABLE`, `}, cacheTTL);`,
		`setTimeout(() => {`,
		`revision === expectedRevision`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("extension does not show routing status (%s missing): %s", want, content)
		}
	}
	if strings.Contains(content, "dari_route") {
		t.Fatal("extension contains the secret key")
	}
	if content != extension {
		t.Error("extension differs from agents/pi/extension.ts")
	}
	if agents.EnvironmentValue(command.Env, "DARI_STATUS_EXECUTABLE") == "" || agents.EnvironmentValue(command.Env, "DARI_CACHE_TTL_MS") != "300000" {
		t.Errorf("status environment = %q, %q", agents.EnvironmentValue(command.Env, "DARI_STATUS_EXECUTABLE"), agents.EnvironmentValue(command.Env, "DARI_CACHE_TTL_MS"))
	}

	command.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary extension still exists: %v", err)
	}
}

// Runs the generated extension with a fake Pi host. A header-only response
// after a warm turn must not cancel the TTL re-render.
func TestPiExtensionCacheExpiryAfterHeaderUpdate(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	path, err := writeExtension()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source := string(data)
	if err := os.WriteFile(filepath.Join(dir, "ext.ts"), []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import ext from "./ext.ts";
const handlers: Record<string, Function> = {};
const statuses: string[] = [];
ext({
  registerProvider() {},
  on(name: string, fn: Function) { handlers[name] = fn; },
  async exec(_: string, args: string[]) {
    const model = JSON.parse(args[2])["x-dari-selected-model"];
    const i = args.indexOf("--cached-tokens");
    const warm = i >= 0 && Number(args[i + 1]) > 0 && Date.now() - Number(args[i + 3]) < 50;
    return { code: 0, stdout: model + " · " + (warm ? "cache warm" : "cache cold") };
  },
});
const ctx = { hasUI: true, ui: { theme: { fg: (_: string, s: string) => s }, setStatus: (_: string, s: string) => statuses.push(s) } };
const headers = { "x-dari-selected-model": "m" };
await handlers.after_provider_response({ headers }, ctx);
await handlers.message_end({ message: { role: "assistant", provider: "dari", usage: { cacheRead: 5 } } }, ctx);
const warm = statuses[statuses.length - 1];
await handlers.after_provider_response({ headers }, ctx);
await new Promise(r => setTimeout(r, 200));
console.log(warm + "|" + statuses[statuses.length - 1]);
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bun, "run.ts")
	cmd.Dir = dir
	cmd.Env = agents.WithEnvironment(os.Environ(), map[string]string{"DARI_STATUS_EXECUTABLE": "dari", "DARI_CACHE_TTL_MS": "50"})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "m · cache warm|m · cache cold" {
		t.Fatalf("statuses = %q", got)
	}
}

// A header render that computed "cache warm" just before expiry and finishes
// after the TTL re-render must not leave the footer warm.
func TestPiExtensionExpiryBeatsInFlightWarmRender(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun not installed")
	}
	path, err := writeExtension()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ext.ts"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	// Warm renders are slow, cold renders fast.
	harness := `import ext from "./ext.ts";
const handlers: Record<string, Function> = {};
const statuses: string[] = [];
ext({
  registerProvider() {},
  on(name: string, fn: Function) { handlers[name] = fn; },
  async exec(_: string, args: string[]) {
    const i = args.indexOf("--cached-tokens");
    const warm = i >= 0 && Number(args[i + 1]) > 0 && Date.now() - Number(args[i + 3]) < 300;
    if (warm) await Bun.sleep(250);
    return { code: 0, stdout: "m · " + (warm ? "cache warm" : "cache cold") };
  },
});
const ctx = { hasUI: true, ui: { theme: { fg: (_: string, s: string) => s }, setStatus: (_: string, s: string) => statuses.push(s) } };
const headers = { "x-dari-selected-model": "m" };
await handlers.after_provider_response({ headers }, ctx);
await handlers.message_end({ message: { role: "assistant", provider: "dari", usage: { cacheRead: 5 } } }, ctx);
await Bun.sleep(0);
handlers.after_provider_response({ headers }, ctx); // Computes warm, finishes after expiry.
await Bun.sleep(700);
console.log(statuses[statuses.length - 1]);
process.exit(0);
`
	if err := os.WriteFile(filepath.Join(dir, "run.ts"), []byte(harness), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bun, "run.ts")
	cmd.Dir = dir
	cmd.Env = agents.WithEnvironment(os.Environ(), map[string]string{"DARI_STATUS_EXECUTABLE": "dari", "DARI_CACHE_TTL_MS": "300"})
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bun: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "m · cache cold" {
		t.Fatalf("final status = %q", got)
	}
}

func TestPiScopesModelPickerToTheRouter(t *testing.T) {
	command := agents.Command{Path: "/pi"}
	if err := Configure(&command, agents.Models{Enabled: []string{"openai/gpt-6-luna", "anthropic/claude-sonnet-5-5"}}); err != nil {
		t.Fatal(err)
	}
	defer command.Cleanup()
	index := slices.Index(command.Args, "--models")
	if index < 0 || command.Args[index+1] != "dari/dari/model/openai/gpt-6-luna,dari/dari/model/anthropic/claude-sonnet-5-5" {
		t.Fatalf("args = %q, want the router's models scoped", command.Args)
	}

	// A scope the user chose stays theirs.
	command = agents.Command{Path: "/pi", Args: []string{"--models", "mine/*"}}
	if err := Configure(&command, agents.Models{Routing: true}); err != nil {
		t.Fatal(err)
	}
	defer command.Cleanup()
	if strings.Count(strings.Join(command.Args, " "), "--models") != 1 {
		t.Fatalf("args = %q, want the user's --models only", command.Args)
	}
}
