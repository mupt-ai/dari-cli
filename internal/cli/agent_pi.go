package cli

import (
	"fmt"
	"os"
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
}
`

func writePiProviderExtension() (string, error) {
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
	if _, err = file.WriteString(piProviderExtension); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Pi provider: %w", err)
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("close Pi provider: %w", err)
	}
	return path, nil
}
