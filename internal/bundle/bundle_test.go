package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildRequiresDariYml(t *testing.T) {
	dir := t.TempDir()
	// No dari.yml → should fail.
	_, err := Build(dir)
	if err == nil {
		t.Fatal("expected error when dari.yml is missing")
	}
}

func TestBuildIncludesFilesAndIsDeterministic(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\n", 0o644)
	writeFile(t, filepath.Join(dir, "prompts", "system.md"), "hello", 0o644)
	writeFile(t, filepath.Join(dir, "run.sh"), "#!/bin/sh\necho hi\n", 0o755)

	// Excluded in fallback selection:
	writeFile(t, filepath.Join(dir, "node_modules", "ignored.js"), "x", 0o644)
	writeFile(t, filepath.Join(dir, ".DS_Store"), "x", 0o644)

	a1, err := Build(dir)
	if err != nil {
		t.Fatalf("Build 1: %v", err)
	}
	a2, err := Build(dir)
	if err != nil {
		t.Fatalf("Build 2: %v", err)
	}
	if a1.SHA256 != a2.SHA256 {
		t.Errorf("non-deterministic: %s vs %s", a1.SHA256, a2.SHA256)
	}
	if !bytes.Equal(a1.Content, a2.Content) {
		t.Errorf("non-deterministic content")
	}

	// Verify file membership + modes by reading the tar.gz back.
	members := readTar(t, a1.Content)
	wantModes := map[string]int64{
		"dari.yml":          0o644,
		"prompts/system.md": 0o644,
		"run.sh":            0o755,
	}
	if len(members) != len(wantModes) {
		t.Errorf("want %d members, got %d: %v", len(wantModes), len(members), members)
	}
	for name, wantMode := range wantModes {
		got, ok := members[name]
		if !ok {
			t.Errorf("missing %q in archive", name)
			continue
		}
		if got.mode != wantMode {
			t.Errorf("%s mode: got %o want %o", name, got.mode, wantMode)
		}
		if got.modTime != 0 {
			t.Errorf("%s mtime: got %d, want 0", name, got.modTime)
		}
		if got.uid != 0 || got.gid != 0 || got.uname != "" || got.gname != "" {
			t.Errorf("%s not normalized: %+v", name, got)
		}
	}

	// Excluded paths must not appear.
	if _, ok := members["node_modules/ignored.js"]; ok {
		t.Errorf("excluded file leaked into archive")
	}
	if _, ok := members[".DS_Store"]; ok {
		t.Errorf(".DS_Store leaked into archive")
	}
}

func TestBuildRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dari.yml"), "name: t\n", 0o644)
	target := filepath.Join(dir, "target.txt")
	writeFile(t, target, "x", 0o644)
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := Build(dir)
	if err == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestBuildAddsCodeFirstTypeScriptToolToManifest(t *testing.T) {
	if _, _, err := codeFirstToolExtractorCommand("helper.mjs", "tool.ts"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\ninstructions:\n  system: prompts/system.md\n", 0o644)
	writeFile(t, filepath.Join(dir, "prompts", "system.md"), "hello", 0o644)
	writeFile(t, filepath.Join(dir, "tools", "repo_search.ts"), `export const description = "Search the repository.";
export const inputSchema = {
  type: "object",
  properties: { query: { type: "string" } },
  required: ["query"]
};
export const outputSchema = {
  type: "object",
  properties: { matches: { type: "array", items: { type: "string" } } }
};
export async function handler(input: { query: string }) {
  return { matches: [input.query] };
}
`, 0o644)

	archive, err := Build(dir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	contents := readTarContents(t, archive.Content)
	for _, name := range []string{
		"dari.yml",
		"tools/repo_search.ts",
	} {
		if _, ok := contents[name]; !ok {
			t.Fatalf("missing path %s; got %v", name, keys(contents))
		}
	}
	for _, generated := range []string{
		"tools/repo_search/tool.yml",
		"tools/repo_search/input.schema.json",
		"tools/repo_search/output.schema.json",
		"tools/repo_search/handler.ts",
	} {
		if _, ok := contents[generated]; ok {
			t.Fatalf("unexpected generated compatibility file %s", generated)
		}
	}
	manifest := contents["dari.yml"]
	for _, want := range []string{
		"custom_tools:",
		"name: repo_search",
		"path: tools/repo_search.ts",
		"runtime: typescript",
		"handler: tools/repo_search.ts:handler",
		"description: Search the repository.",
		"input_schema_json:",
		"output_schema_json:",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("generated manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestBuildSupportsFolderCodeFirstTypeScriptTool(t *testing.T) {
	if _, _, err := codeFirstToolExtractorCommand("helper.mjs", "tool.ts"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\n", 0o644)
	writeFile(t, filepath.Join(dir, "tools", "repo_search", "tool.ts"), `import { normalize } from "./helpers.ts";
export const description = "Search.";
export const inputSchema = { type: "object", properties: { query: { type: "string" } } };
export function handler(input: { query: string }) { return { query: normalize(input.query) }; }
`, 0o644)
	writeFile(t, filepath.Join(dir, "tools", "repo_search", "helpers.ts"), `export function normalize(value: string) { return value.trim(); }
`, 0o644)

	archive, err := Build(dir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	contents := readTarContents(t, archive.Content)
	if _, ok := contents["tools/repo_search/helpers.ts"]; !ok {
		t.Fatalf("helper file should stay in uploaded bundle; got %v", keys(contents))
	}
	manifest := contents["dari.yml"]
	for _, want := range []string{
		"path: tools/repo_search",
		"handler: tools/repo_search/tool.ts:handler",
		"input_schema_json:",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("generated manifest missing %q:\n%s", want, manifest)
		}
	}
}

func TestBuildCompletesPartialCodeFirstToolManifestEntry(t *testing.T) {
	if _, _, err := codeFirstToolExtractorCommand("helper.mjs", "tool.ts"); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "dari.yml"), "name: test\nharness: pi\ncustom_tools:\n  - name: repo_search\n    path: tools/repo_search.ts\n    timeout_seconds: 20\n", 0o644)
	writeFile(t, filepath.Join(dir, "tools", "repo_search.ts"), `export const description = "Search.";
export const inputSchema = { type: "object", properties: { query: { type: "string" } } };
export function handler(input: { query: string }) { return { query: input.query }; }
`, 0o644)

	archive, err := Build(dir)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	manifest := readTarContents(t, archive.Content)["dari.yml"]
	for _, want := range []string{
		"name: repo_search",
		"path: tools/repo_search.ts",
		"kind: main",
		"runtime: typescript",
		"handler: tools/repo_search.ts:handler",
		"description: Search.",
		"input_schema_json:",
		"timeout_seconds: 20",
	} {
		if !strings.Contains(manifest, want) {
			t.Fatalf("generated manifest missing %q:\n%s", want, manifest)
		}
	}
}

type member struct {
	mode    int64
	modTime int64
	uid     int
	gid     int
	uname   string
	gname   string
	size    int64
}

func readTar(t *testing.T, data []byte) map[string]member {
	t.Helper()
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gzr)
	out := map[string]member{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		out[hdr.Name] = member{
			mode:    hdr.Mode,
			modTime: hdr.ModTime.Unix(),
			uid:     hdr.Uid,
			gid:     hdr.Gid,
			uname:   hdr.Uname,
			gname:   hdr.Gname,
			size:    hdr.Size,
		}
	}
	return out
}

func readTarContents(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gzr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gzr)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = string(content)
	}
	return out
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

func writeFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
}
