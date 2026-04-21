// Package scaffold generates a new Dari agent project. The templates are
// inlined rather than embedded from disk because they're small and it avoids
// a separate embed.FS indirection for two interpolations.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	defaultSkillName   = "review"
	defaultProjectName = "my-agent"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Options configures a single scaffold run.
type Options struct {
	TargetDir string
	Name      string // optional; inferred from TargetDir when empty
	Skill     string // defaults to "review"
	Force     bool
}

// Result is what `dari init` prints.
type Result struct {
	ProjectRoot  string
	ProjectName  string
	SkillName    string
	WrittenFiles []string // relative to ProjectRoot, POSIX-separator, sorted
}

// Run scaffolds the project and returns what was written.
func Run(opts Options) (*Result, error) {
	targetDir := opts.TargetDir
	if targetDir == "" {
		targetDir = "."
	}
	projectRoot, err := filepath.Abs(targetDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(projectRoot, 0o755); err != nil {
		return nil, err
	}

	projectName, err := resolveProjectName(opts.Name, projectRoot)
	if err != nil {
		return nil, err
	}
	skillName := opts.Skill
	if skillName == "" {
		skillName = defaultSkillName
	}
	if !namePattern.MatchString(skillName) {
		return nil, fmt.Errorf("skill name must match [a-z0-9][a-z0-9-]* (got %q)", opts.Skill)
	}

	files := renderFiles(projectName, skillName)

	if !opts.Force {
		var conflicts []string
		for _, f := range files {
			if _, err := os.Stat(filepath.Join(projectRoot, f.path)); err == nil {
				conflicts = append(conflicts, f.path)
			}
		}
		if len(conflicts) > 0 {
			return nil, fmt.Errorf("refusing to overwrite existing files (pass --force to overwrite):\n  %s", strings.Join(conflicts, "\n  "))
		}
	}

	var written []string
	for _, f := range files {
		dest := filepath.Join(projectRoot, f.path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, []byte(f.content), 0o644); err != nil {
			return nil, err
		}
		written = append(written, f.path)
	}
	return &Result{
		ProjectRoot:  projectRoot,
		ProjectName:  projectName,
		SkillName:    skillName,
		WrittenFiles: written,
	}, nil
}

func resolveProjectName(explicit, projectRoot string) (string, error) {
	if explicit != "" {
		trimmed := strings.TrimSpace(explicit)
		if !namePattern.MatchString(trimmed) {
			return "", fmt.Errorf("project name must match [a-z0-9][a-z0-9-]* (got %q). Pass --name to override", explicit)
		}
		return trimmed, nil
	}
	candidate := slugify(filepath.Base(projectRoot))
	if candidate == "" {
		candidate = defaultProjectName
	}
	if !namePattern.MatchString(candidate) {
		return "", fmt.Errorf("project name must match [a-z0-9][a-z0-9-]* (got %q). Pass --name to override", candidate)
	}
	return candidate, nil
}

func slugify(v string) string {
	lowered := strings.ToLower(strings.TrimSpace(v))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return strings.Trim(re.ReplaceAllString(lowered, "-"), "-")
}

type renderedFile struct {
	path    string
	content string
}

func renderFiles(projectName, skillName string) []renderedFile {
	return []renderedFile{
		{"dari.yml", renderDariYml(projectName, skillName)},
		{"Dockerfile", renderDockerfile()},
		{"prompts/system.md", renderSystemPrompt(skillName)},
		{"tools/repo_search/tool.yml", renderToolYml()},
		{"tools/repo_search/input.schema.json", renderToolInputSchema()},
		{"tools/repo_search/handler.ts", renderToolHandler()},
		{filepath.ToSlash(filepath.Join("skills", skillName, "SKILL.md")), renderSkillMd(skillName)},
		{"README.md", renderReadme(projectName, skillName)},
		{".gitignore", renderGitignore()},
	}
}

func renderDariYml(projectName, skillName string) string {
	return fmt.Sprintf(`name: %s
harness: pi

instructions:
  system: prompts/system.md

runtime:
  dockerfile: Dockerfile

sandbox:
  provider: e2b
  provider_api_key_secret: E2B_API_KEY

llm:
  model: anthropic/claude-sonnet-4.6
  base_url: https://openrouter.ai/api/v1
  api_key_secret: OPENROUTER_API_KEY

tools:
  - name: repo_search
    path: tools/repo_search
    kind: main

skills:
  - name: %s
    path: skills/%s

env:
  APP_ENV: example
`, projectName, skillName, skillName)
}

func renderDockerfile() string {
	return `FROM node:20-bookworm

WORKDIR /bundle
COPY . /bundle
`
}

func renderSystemPrompt(skillName string) string {
	return fmt.Sprintf(`You are a managed agent bundle.
Use `+"`repo_search`"+` before answering questions about checked-in files.
Load the `+"`%s`"+` skill when the user asks you to apply it.
`, skillName)
}

func renderToolYml() string {
	return `name: repo_search
description: Search the checked-out repository for matching content.
input_schema: input.schema.json
runtime: typescript
handler: handler.ts:main
retries: 2
timeout_seconds: 20
`
}

func renderToolInputSchema() string {
	return `{
  "type": "object",
  "properties": {
    "query": {
      "type": "string"
    }
  },
  "required": ["query"],
  "additionalProperties": false
}
`
}

func renderToolHandler() string {
	return `export async function main(input: { query: string }) {
  return {
    matches: [` + "`matched: ${input.query}`" + `],
  };
}
`
}

func renderSkillMd(skillName string) string {
	title := titleCase(strings.ReplaceAll(skillName, "-", " "))
	return fmt.Sprintf(`---
name: %s
description: %s playbook for this agent.
---

# %s

Steps to follow when the agent is asked to apply this skill:

1. Gather the relevant files with `+"`repo_search`"+`.
2. Summarize what you found before taking action.
3. Reply with concrete, specific recommendations.
`, skillName, title, title)
}

func renderReadme(projectName, skillName string) string {
	return fmt.Sprintf("# %s\n\nScaffolded with `dari init`. Validate and deploy from this directory:\n\n```bash\ndari deploy --dry-run\ndari deploy\n```\n\nBefore a real deploy, upload any referenced credentials:\n\n```bash\ndari credentials add OPENROUTER_API_KEY\ndari credentials add E2B_API_KEY\n```\n\n## Layout\n\n- `dari.yml` — manifest\n- `prompts/system.md` — system prompt\n- `tools/repo_search/` — example tool\n- `skills/%s/SKILL.md` — example skill\n", projectName, skillName)
}

func renderGitignore() string {
	return ".dari/\n.env\n"
}

// titleCase mirrors Python's str.title() — uppercases the first character of
// each whitespace-delimited word, lowercases the rest.
func titleCase(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}
