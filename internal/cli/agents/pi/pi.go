// Package pi launches Pi with a Dari provider extension that shows routing
// status in Pi's footer. The extension is extension.ts, embedded as written.
package pi

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

//go:embed extension.ts
var extension string

// Configure adds the Dari provider extension and selects dari/routing.
func Configure(command *agents.Command) error {
	status, err := agents.StatusEnvironment()
	if err != nil {
		return err
	}
	extensionPath, err := writeExtension()
	if err != nil {
		return err
	}
	command.Env = agents.WithEnvironment(command.Env, status)
	command.Cleanup = func() { _ = os.Remove(extensionPath) }
	command.Args = agents.InsertBeforeDoubleDash(command.Args, []string{
		"--extension", extensionPath,
		"--provider", "dari",
		"--model", agents.RoutingModel,
	})
	return nil
}

func writeExtension() (path string, err error) {
	file, err := os.CreateTemp("", "dari-pi-provider-*.ts")
	if err != nil {
		return "", fmt.Errorf("create temporary Pi provider: %w", err)
	}
	path = file.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	if err = file.Chmod(0o600); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("set Pi provider permissions: %w", err)
	}
	if _, err = file.WriteString(extension); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write Pi provider: %w", err)
	}
	if err = file.Close(); err != nil {
		return "", fmt.Errorf("close Pi provider: %w", err)
	}
	return path, nil
}
