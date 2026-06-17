package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "chat")
	result, err := Run(Options{TargetDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ProjectName != "chat" {
		t.Errorf("project name: %q", result.ProjectName)
	}
	mustExist := []string{
		"dari.yml",
		"package.json",
		"agents/chat.ts",
		"README.md",
		".gitignore",
	}
	for _, p := range mustExist {
		full := filepath.Join(dir, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	yml, _ := os.ReadFile(filepath.Join(dir, "dari.yml"))
	expected := "name: chat\nsandbox:\n  secrets:\n    - OPENROUTER_API_KEY\n"
	if got := string(yml); got != expected {
		t.Errorf("dari.yml = %q, want generated Flue manifest", got)
	}
	for _, oldSurface := range []string{"harness:", "flue:", "routing:", "custom_tools:", "skills:", "built_in_tools:"} {
		if strings.Contains(string(yml), oldSurface) {
			t.Errorf("dari.yml should not contain %q:\n%s", oldSurface, yml)
		}
	}
}

func TestRunRejectsInvalidProjectName(t *testing.T) {
	dir := t.TempDir()
	_, err := Run(Options{TargetDir: dir, Name: "BAD NAME"})
	if err == nil {
		t.Fatal("expected error for invalid project name")
	}
}

func TestRunRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "acme")
	if _, err := Run(Options{TargetDir: dir}); err != nil {
		t.Fatal(err)
	}
	_, err := Run(Options{TargetDir: dir})
	if err == nil {
		t.Fatal("expected conflict error on second run without --force")
	}
	if _, err := Run(Options{TargetDir: dir, Force: true}); err != nil {
		t.Errorf("--force run failed: %v", err)
	}
}

func TestRunUsesExplicitNameForManifestAndAgentFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "support")
	r, err := Run(Options{TargetDir: dir, Name: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	if r.ProjectName != "chat" {
		t.Errorf("project name: %q", r.ProjectName)
	}
	if _, err := os.Stat(filepath.Join(dir, "agents", "chat.ts")); err != nil {
		t.Errorf("missing agent file: %v", err)
	}
	yml, _ := os.ReadFile(filepath.Join(dir, "dari.yml"))
	expected := "name: chat\nsandbox:\n  secrets:\n    - OPENROUTER_API_KEY\n"
	if string(yml) != expected {
		t.Errorf("dari.yml = %q, want explicit name with declared OpenRouter secret", yml)
	}
}
