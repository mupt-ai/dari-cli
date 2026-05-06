// Package scaffold generates a new Dari agent project from the embedded
// templates in templates/.
package scaffold

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

const (
	defaultSkillName   = "review"
	defaultProjectName = "my-agent"
)

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

//go:embed templates
var templatesFS embed.FS

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

// templateData is what every template in templates/ renders against.
type templateData struct {
	ProjectName string
	SkillName   string
	SkillTitle  string
}

// fileSpec maps an embedded source template to its output path. Both the
// source and the output path are rendered through text/template so the skill
// directory name can depend on SkillName.
type fileSpec struct {
	source     string // path inside templatesFS
	outputPath string // POSIX path relative to ProjectRoot; may contain template actions
}

var fileSpecs = []fileSpec{
	{"templates/dari.yml.tmpl", "dari.yml"},
	{"templates/system.md.tmpl", "prompts/system.md"},
	{"templates/tool.yml", "tools/repo_search/tool.yml"},
	{"templates/input.schema.json", "tools/repo_search/input.schema.json"},
	{"templates/handler.ts", "tools/repo_search/handler.ts"},
	{"templates/SKILL.md.tmpl", "skills/{{.SkillName}}/SKILL.md"},
	{"templates/README.md.tmpl", "README.md"},
	{"templates/gitignore", ".gitignore"},
}

// Run scaffolds the project and returns what was written.
func Run(opts Options) (*Result, error) {
	projectRoot, err := resolveProjectRoot(opts.TargetDir)
	if err != nil {
		return nil, err
	}
	projectName, err := resolveProjectName(opts.Name, projectRoot)
	if err != nil {
		return nil, err
	}
	skillName, err := resolveSkillName(opts.Skill)
	if err != nil {
		return nil, err
	}

	data := templateData{
		ProjectName: projectName,
		SkillName:   skillName,
		SkillTitle:  titleCase(strings.ReplaceAll(skillName, "-", " ")),
	}
	files, err := renderFiles(data)
	if err != nil {
		return nil, err
	}

	if !opts.Force {
		if err := checkConflicts(projectRoot, files); err != nil {
			return nil, err
		}
	}

	written, err := writeAll(projectRoot, files)
	if err != nil {
		return nil, err
	}
	return &Result{
		ProjectRoot:  projectRoot,
		ProjectName:  projectName,
		SkillName:    skillName,
		WrittenFiles: written,
	}, nil
}

func resolveProjectRoot(targetDir string) (string, error) {
	if targetDir == "" {
		targetDir = "."
	}
	abs, err := filepath.Abs(targetDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return abs, nil
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

func resolveSkillName(explicit string) (string, error) {
	if explicit == "" {
		return defaultSkillName, nil
	}
	if !namePattern.MatchString(explicit) {
		return "", fmt.Errorf("skill name must match [a-z0-9][a-z0-9-]* (got %q)", explicit)
	}
	return explicit, nil
}

type renderedFile struct {
	path    string
	content []byte
}

func renderFiles(data templateData) ([]renderedFile, error) {
	out := make([]renderedFile, 0, len(fileSpecs))
	for _, spec := range fileSpecs {
		path, err := renderString(spec.outputPath, data)
		if err != nil {
			return nil, fmt.Errorf("render output path for %s: %w", spec.source, err)
		}
		raw, err := templatesFS.ReadFile(spec.source)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", spec.source, err)
		}
		content, err := renderTemplate(spec.source, string(raw), data)
		if err != nil {
			return nil, err
		}
		out = append(out, renderedFile{path: filepath.ToSlash(path), content: content})
	}
	return out, nil
}

func renderString(s string, data templateData) (string, error) {
	out, err := renderTemplate("<path>", s, data)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func renderTemplate(name, body string, data templateData) ([]byte, error) {
	t, err := template.New(name).Option("missingkey=error").Parse(body)
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template %s: %w", name, err)
	}
	return buf.Bytes(), nil
}

func checkConflicts(projectRoot string, files []renderedFile) error {
	var conflicts []string
	for _, f := range files {
		if _, err := os.Stat(filepath.Join(projectRoot, f.path)); err == nil {
			conflicts = append(conflicts, f.path)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("refusing to overwrite existing files (pass --force to overwrite):\n  %s", strings.Join(conflicts, "\n  "))
	}
	return nil
}

func writeAll(projectRoot string, files []renderedFile) ([]string, error) {
	written := make([]string, 0, len(files))
	for _, f := range files {
		dest := filepath.Join(projectRoot, f.path)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(dest, f.content, 0o644); err != nil {
			return nil, err
		}
		written = append(written, f.path)
	}
	return written, nil
}

func slugify(v string) string {
	lowered := strings.ToLower(strings.TrimSpace(v))
	re := regexp.MustCompile(`[^a-z0-9]+`)
	return strings.Trim(re.ReplaceAllString(lowered, "-"), "-")
}

// titleCase mirrors Python's str.title(): uppercases the first character of
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
