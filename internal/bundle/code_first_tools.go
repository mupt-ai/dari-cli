package bundle

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed code_first_tool_extractor.mjs
var codeFirstToolExtractor string

type codeFirstToolSpec struct {
	Description  string         `json:"description"`
	InputSchema  map[string]any `json:"input_schema"`
	OutputSchema map[string]any `json:"output_schema,omitempty"`
}

type codeFirstTool struct {
	Name         string
	Spec         codeFirstToolSpec
	SourcePath   string
	ManifestPath string
}

func buildCodeFirstToolOverlays(root string, selectedPaths []string) (map[string][]byte, []string, error) {
	tools, err := discoverCodeFirstTools(root, selectedPaths)
	if err != nil {
		return nil, nil, err
	}
	if len(tools) == 0 {
		return map[string][]byte{}, selectedPaths, nil
	}

	overlays := make(map[string][]byte)
	manifest, err := buildManifestWithCodeFirstTools(root, tools)
	if err != nil {
		return nil, nil, err
	}
	overlays["dari.yml"] = manifest
	return overlays, selectedPaths, nil
}

func discoverCodeFirstTools(root string, selectedPaths []string) ([]codeFirstTool, error) {
	var tools []codeFirstTool
	seenNames := map[string]string{}
	for _, rel := range selectedPaths {
		name, ok := codeFirstToolName(rel)
		if !ok {
			continue
		}
		if previous, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("code-first tool name %q is defined by both %s and %s", name, previous, rel)
		}
		seenNames[name] = rel
		spec, err := extractCodeFirstToolSpec(root, rel)
		if err != nil {
			return nil, err
		}
		manifestPath := rel
		if path.Base(rel) == "tool.ts" || path.Base(rel) == "tool.mts" {
			manifestPath = path.Dir(rel)
		}
		tools = append(tools, codeFirstTool{
			Name:         name,
			Spec:         spec,
			SourcePath:   rel,
			ManifestPath: manifestPath,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].ManifestPath < tools[j].ManifestPath })
	return tools, nil
}

func codeFirstToolName(rel string) (string, bool) {
	if !strings.HasPrefix(rel, "tools/") || strings.HasSuffix(rel, ".d.ts") {
		return "", false
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 2 {
		return codeFirstToolNameFromDirectFile(parts[1])
	}
	if len(parts) == 3 && (parts[2] == "tool.ts" || parts[2] == "tool.mts") {
		name := parts[1]
		if name != "" {
			return name, true
		}
	}
	return "", false
}

func codeFirstToolNameFromDirectFile(base string) (string, bool) {
	var name string
	switch {
	case strings.HasSuffix(base, ".tool.ts"):
		name = strings.TrimSuffix(base, ".tool.ts")
	case strings.HasSuffix(base, ".ts"):
		name = strings.TrimSuffix(base, ".ts")
	case strings.HasSuffix(base, ".tool.mts"):
		name = strings.TrimSuffix(base, ".tool.mts")
	case strings.HasSuffix(base, ".mts"):
		name = strings.TrimSuffix(base, ".mts")
	default:
		return "", false
	}
	if name == "" || name == "handler" || name == "index" {
		return "", false
	}
	return name, true
}

func extractCodeFirstToolSpec(root, rel string) (codeFirstToolSpec, error) {
	helper, err := os.CreateTemp("", "dari-tool-extractor-*.mjs")
	if err != nil {
		return codeFirstToolSpec{}, err
	}
	defer os.Remove(helper.Name())
	if _, err := helper.WriteString(codeFirstToolExtractor); err != nil {
		helper.Close()
		return codeFirstToolSpec{}, err
	}
	if err := helper.Close(); err != nil {
		return codeFirstToolSpec{}, err
	}

	cmdName, args, err := codeFirstToolExtractorCommand(helper.Name(), filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return codeFirstToolSpec{}, err
	}
	cmd := exec.Command(cmdName, args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return codeFirstToolSpec{}, fmt.Errorf("extract code-first tool %s: %s", rel, message)
	}
	var spec codeFirstToolSpec
	if err := json.Unmarshal(out, &spec); err != nil {
		return codeFirstToolSpec{}, fmt.Errorf("extract code-first tool %s returned invalid JSON: %w", rel, err)
	}
	return spec, nil
}

func codeFirstToolExtractorCommand(helperPath, modulePath string) (string, []string, error) {
	if tsx, err := exec.LookPath("tsx"); err == nil {
		return tsx, []string{helperPath, modulePath}, nil
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return "", nil, errors.New("code-first TypeScript tools require either tsx or Node.js with --experimental-strip-types on PATH")
	}
	if err := exec.Command(node, "--experimental-strip-types", "-e", "").Run(); err != nil {
		return "", nil, errors.New("code-first TypeScript tools require tsx or a Node.js version that supports --experimental-strip-types")
	}
	return node, []string{"--experimental-strip-types", helperPath, modulePath}, nil
}

func buildManifestWithCodeFirstTools(root string, tools []codeFirstTool) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(root, "dari.yml"))
	if err != nil {
		return nil, err
	}
	manifest := map[string]any{}
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse dari.yml for code-first tools: %w", err)
	}
	var entries []any
	if raw, ok := manifest["custom_tools"]; ok && raw != nil {
		var okList bool
		entries, okList = raw.([]any)
		if !okList {
			return nil, errors.New("dari.yml custom_tools must be a list when using code-first tools")
		}
	}
	for _, tool := range tools {
		generated := generatedCodeFirstToolEntry(tool)
		if entry := matchingManifestToolEntry(entries, tool); entry != nil {
			mergeMissingManifestToolFields(entry, generated)
			continue
		}
		entries = append(entries, generated)
	}
	manifest["custom_tools"] = entries
	out, err := yaml.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func generatedCodeFirstToolEntry(tool codeFirstTool) map[string]any {
	entry := map[string]any{
		"name":              tool.Name,
		"path":              tool.ManifestPath,
		"kind":              "main",
		"runtime":           "typescript",
		"handler":           tool.SourcePath + ":handler",
		"description":       tool.Spec.Description,
		"input_schema_json": tool.Spec.InputSchema,
	}
	if tool.Spec.OutputSchema != nil {
		entry["output_schema_json"] = tool.Spec.OutputSchema
	}
	return entry
}

func matchingManifestToolEntry(entries []any, tool codeFirstTool) map[string]any {
	for _, entry := range entries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		if m["path"] == tool.ManifestPath || m["name"] == tool.Name {
			return m
		}
	}
	return nil
}

func mergeMissingManifestToolFields(entry, generated map[string]any) {
	for key, value := range generated {
		if _, exists := entry[key]; !exists || entry[key] == nil {
			entry[key] = value
		}
	}
}
