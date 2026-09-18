package cli

import (
	"os"
	"slices"
	"strings"
	"testing"
)

func TestBuildPiAgentCommandUsesTemporaryProviderExtension(t *testing.T) {
	command, err := buildAgentCommand(
		"/pi",
		agentLaunch{name: "pi", args: []string{"-p", "review"}},
		"dari_route",
		"default",
	)
	if err != nil {
		t.Fatal(err)
	}
	if command.cleanup == nil {
		t.Fatal("Pi command has no cleanup")
	}

	extensionIndex := slices.Index(command.args, "--extension")
	if extensionIndex < 0 || extensionIndex+1 >= len(command.args) {
		t.Fatalf("args do not contain an extension: %q", command.args)
	}
	path := command.args[extensionIndex+1]
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, `pi.registerProvider("dari"`) {
		t.Errorf("extension does not register Dari: %s", content)
	}
	if strings.Contains(content, "dari_route") {
		t.Fatal("extension contains the secret key")
	}

	command.cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("temporary extension still exists: %v", err)
	}
}
