package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func configurePiCommand(command *agentCommand) error {
	extensionPath, err := writePiProviderExtension()
	if err != nil {
		return err
	}
	command.cleanup = func() { _ = os.Remove(extensionPath) }
	command.args = insertBeforeDoubleDash(command.args, []string{
		"--extension", extensionPath,
		"--provider", "dari",
		"--model", routingModel,
	})
	return nil
}

// The extension registers the Dari provider and mirrors each response's
// routing headers into Pi's footer: the model that served the turn, its
// thinking level, and how many turns remain on the current lease.
const piProviderExtension = `export default function (pi: any) {
  pi.registerProvider("dari", {
    name: "Dari",
    baseUrl: "https://routing.dari.dev/v1",
    apiKey: "$DARI_ROUTING_API_KEY",
    api: "openai-completions",
    compat: { sendSessionAffinityHeaders: true },
    models: [{
      id: "dari/routing",
      name: "Dari Router",
      reasoning: false,
      input: ["text", "image"],
      cost: { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 },
      contextWindow: 1000000,
      maxTokens: 128000
    }]
  });

  pi.on("after_provider_response", async (event: any, ctx: any) => {
    if (!ctx.hasUI || !event.headers?.["x-dari-selected-model"]) return;
    const headers = Object.fromEntries([
      "x-dari-selected-model", "x-dari-thinking-level", "x-dari-lease-turns-remaining"
    ].filter(name => event.headers[name] !== undefined).map(name => [name, event.headers[name]]));
    const result = await pi.exec(DARI_STATUS_EXECUTABLE, [
      "agent-status", "--headers", JSON.stringify(headers)
    ], { timeout: 2000 });
    if (result.code === 0) {
      const parts = result.stdout.trim().split(" · ");
      ctx.ui.setStatus("dari", parts.map((part: string, index: number) =>
        ctx.ui.theme.fg(index === 0 ? "accent" : "dim", part)
      ).join(" · "));
    }
  });
}
`

func writePiProviderExtension() (string, error) {
	executable, err := agentStatusExecutable()
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(executable)
	if err != nil {
		return "", err
	}
	content := strings.ReplaceAll(piProviderExtension, "DARI_STATUS_EXECUTABLE", string(encoded))
	file, err := os.CreateTemp("", "dari-pi-provider-*.ts")
	if err != nil {
		return "", fmt.Errorf("create temporary Pi provider: %w", err)
	}
	path := file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("set Pi provider permissions: %w", err)
	}
	if _, err = file.WriteString(content); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Pi provider: %w", err)
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("close Pi provider: %w", err)
	}
	return path, nil
}
