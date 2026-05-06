package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "my-agent")
	result, err := Run(Options{TargetDir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.ProjectName != "my-agent" {
		t.Errorf("project name: %q", result.ProjectName)
	}
	if result.SkillName != "review" {
		t.Errorf("skill: %q", result.SkillName)
	}
	mustExist := []string{
		"dari.yml", "prompts/system.md",
		"tools/repo_search/tool.yml", "tools/repo_search/input.schema.json",
		"tools/repo_search/handler.ts", "skills/review/SKILL.md",
		"README.md", ".gitignore",
	}
	for _, p := range mustExist {
		full := filepath.Join(dir, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	yml, _ := os.ReadFile(filepath.Join(dir, "dari.yml"))
	if !strings.Contains(string(yml), "name: my-agent") {
		t.Errorf("dari.yml does not interpolate project name: %s", yml)
	}
	if strings.Contains(string(yml), "dockerfile:") {
		t.Errorf("dari.yml should use the default E2B runtime instead of a Dockerfile: %s", yml)
	}
	if _, err := os.Stat(filepath.Join(dir, "Dockerfile")); !os.IsNotExist(err) {
		t.Errorf("Dockerfile should not be scaffolded by default; stat err=%v", err)
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

func TestCustomSkillName(t *testing.T) {
	dir := t.TempDir()
	r, err := Run(Options{TargetDir: dir, Skill: "incident-review"})
	if err != nil {
		t.Fatal(err)
	}
	if r.SkillName != "incident-review" {
		t.Errorf("skill: %q", r.SkillName)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills", "incident-review", "SKILL.md")); err != nil {
		t.Errorf("missing skill dir: %v", err)
	}
}
