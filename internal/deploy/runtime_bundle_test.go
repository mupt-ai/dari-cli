package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mupt-ai/dari-cli/internal/bundle"
)

func TestBuildRuntimeArchiveBuildsOnceAndIncludesRuntimeDeps(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("runtime archive build uses bash")
	}
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: chat\n")
	writeDeployFile(t, filepath.Join(dir, "package.json"), "{\"type\":\"module\"}\n")
	writeDeployFile(t, filepath.Join(dir, "package-lock.json"), "{}\n")
	writeDeployFile(t, filepath.Join(dir, "agents", "chat.ts"), "export default {};\n")

	bin := t.TempDir()
	writeExecutable(t, filepath.Join(bin, "npm"), `#!/bin/sh
set -eu
mkdir -p node_modules/.bin
cat > node_modules/.bin/flue <<'FLUE'
#!/bin/sh
set -eu
mkdir -p dist
printf 'export {};\n' > dist/server.mjs
FLUE
chmod +x node_modules/.bin/flue
`)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	sourceArchive, err := bundle.Build(dir)
	if err != nil {
		t.Fatalf("Build source: %v", err)
	}
	runtimeArchive, built, err := buildRuntimeArchive(dir, sourceArchive, nil)
	if err != nil {
		t.Fatalf("buildRuntimeArchive: %v", err)
	}
	if !built {
		t.Fatal("buildRuntimeArchive did not build runtime archive")
	}

	contents := readRuntimeArchive(t, runtimeArchive.Content)
	for _, name := range []string{
		".dari-built",
		"db.ts",
		"dist/server.mjs",
		"node_modules/.bin/flue",
		"dari.yml",
	} {
		if _, ok := contents[name]; !ok {
			t.Fatalf("runtime archive missing %s; got %v", name, mapKeys(contents))
		}
	}
}

func TestBuildRuntimeArchiveSkipsProjectsWithoutPackageJSON(t *testing.T) {
	dir := t.TempDir()
	writeDeployFile(t, filepath.Join(dir, "dari.yml"), "name: chat\n")
	sourceArchive, err := bundle.Build(dir)
	if err != nil {
		t.Fatalf("Build source: %v", err)
	}

	archive, built, err := buildRuntimeArchive(dir, sourceArchive, nil)
	if err != nil {
		t.Fatalf("buildRuntimeArchive: %v", err)
	}
	if built || archive != nil {
		t.Fatalf("buildRuntimeArchive built=%v archive=%v, want no build", built, archive)
	}
}

func readRuntimeArchive(t *testing.T, data []byte) map[string]string {
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

func mapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	return out
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
