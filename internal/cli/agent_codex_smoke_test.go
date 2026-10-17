package cli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Optional installed-Codex smoke test. Run under a terminal/PTY with an isolated
// CODEX_HOME and a local mock endpoint/catalog; never points at a paid provider.
func TestCodexInstalledStatus(t *testing.T) {
	base := os.Getenv("DARI_TEST_CODEX_MOCK_URL")
	if base == "" {
		t.Skip("requires local Codex mock and terminal")
	}
	if !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatal("smoke test requires a loopback mock")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--no-alt-screen", "hello"}
	if encoded := os.Getenv("DARI_TEST_CODEX_ARGS"); encoded != "" {
		if err := json.Unmarshal([]byte(encoded), &args); err != nil {
			t.Fatal(err)
		}
	}
	command, err := buildAgentCommand(path, agentLaunch{name: "codex", args: args}, "test-key", "default")
	if err != nil {
		t.Fatal(err)
	}
	// Point both the catalog and response paths at the explicit mock boundary.
	command.args = insertBeforeDoubleDash(command.args, []string{"--config", `model_providers.dari.base_url="` + base + `/v1"`})
	defer func() {
		if command.cleanup != nil {
			command.cleanup()
		}
	}()
	if err := prepareCodexCatalog(context.Background(), &command, "test-key", base); err != nil {
		t.Fatal(err)
	}
	if err := prepareCodexStatus(context.Background(), &command, args, "test-key", base); err != nil {
		t.Fatal(err)
	}
	process := exec.Command(command.path, command.args...)
	process.Env = command.env
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	if err := process.Run(); err != nil {
		t.Fatal(err)
	}
}
