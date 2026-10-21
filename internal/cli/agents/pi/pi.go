// Package pi launches Pi with a Dari provider extension that shows routing
// status in Pi's footer. The extension is extension.ts, embedded as written.
package pi

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mupt-ai/dari-cli/internal/cli/agents"
)

//go:embed extension.ts
var extension string

// Configure adds the Dari provider extension, which offers the launch's
// models in Pi's model picker, and starts on the default model.
func Configure(command *agents.Command, models agents.Models) error {
	status, err := agents.StatusEnvironment()
	if err != nil {
		return err
	}
	menu, err := json.Marshal(models.Menu())
	if err != nil {
		return fmt.Errorf("encode Pi models: %w", err)
	}
	status["DARI_MODELS"] = string(menu)
	extensionPath, err := writeExtension()
	if err != nil {
		return err
	}
	command.Env = agents.WithEnvironment(command.Env, status)
	command.Cleanup = func() { _ = os.Remove(extensionPath) }
	args := []string{
		"--extension", extensionPath,
		"--provider", "dari",
		"--model", models.Default(),
	}
	// Scoping the router's models makes /model open on them rather than on
	// every provider the user has signed in to.
	if !agents.ArgsContainFlag(command.Args, "--models") {
		scoped := make([]string, 0, len(models.Menu()))
		for _, model := range models.Menu() {
			scoped = append(scoped, "dari/"+model)
		}
		args = append(args, "--models", strings.Join(scoped, ","))
	}
	command.Args = agents.InsertBeforeDoubleDash(command.Args, args)
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
