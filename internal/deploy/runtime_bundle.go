package deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/mupt-ai/dari-cli/internal/bundle"
)

const (
	runtimeArchiveMarker = ".dari-built"
	sourceArchivePath    = ".dari-source.tar.gz"
)

// buildRuntimeArchive returns a prebuilt Flue runtime archive when deployRoot
// looks like a Flue project with a package.json. The archive still contains the
// source manifest, but also carries dist/server.mjs and node_modules so the
// Cloud Run message worker can extract and run instead of installing/building on
// every cold version materialization.
func buildRuntimeArchive(deployRoot string, sourceArchive *bundle.Archive, buildOutput io.Writer) (*bundle.Archive, bool, error) {
	manifest, shouldBuild, err := readFlueManifest(deployRoot)
	if err != nil || !shouldBuild {
		return nil, false, err
	}
	if _, err := os.Stat(filepath.Join(deployRoot, "package.json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if err := requirePortableDependencyStrategy(deployRoot); err != nil {
		return nil, false, err
	}

	workDir, err := os.MkdirTemp("", "dari-flue-build-*")
	if err != nil {
		return nil, false, err
	}
	defer os.RemoveAll(workDir)

	if err := extractArchive(sourceArchive.Content, workDir); err != nil {
		return nil, false, fmt.Errorf("extract source bundle for runtime build: %w", err)
	}
	if err := injectDariPersistence(workDir); err != nil {
		return nil, false, err
	}
	if err := runBuildCommand(workDir, manifest.buildCommand(), buildOutput); err != nil {
		return nil, false, err
	}
	if err := installTargetRuntimeDependencies(workDir, buildOutput); err != nil {
		return nil, false, err
	}
	if err := requireRuntimeBuild(workDir); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(filepath.Join(workDir, runtimeArchiveMarker), []byte("prebuilt\n"), 0o644); err != nil {
		return nil, false, err
	}
	if err := os.WriteFile(filepath.Join(workDir, sourceArchivePath), sourceArchive.Content, 0o644); err != nil {
		return nil, false, err
	}

	archive, err := bundle.BuildWithOptions(workDir, bundle.Options{IncludeNodeModules: true, AllowSymlinks: true})
	if err != nil {
		return nil, false, err
	}
	return archive, true, nil
}

type flueManifest struct {
	Harness any `yaml:"harness"`
	Flue    struct {
		BuildCommand string `yaml:"build_command"`
	} `yaml:"flue"`
}

func (m flueManifest) buildCommand() string {
	if cmd := strings.TrimSpace(m.Flue.BuildCommand); cmd != "" {
		return cmd
	}
	if raw, ok := m.Harness.(map[string]any); ok {
		if flue, ok := raw["flue"].(map[string]any); ok {
			if cmd, ok := flue["build_command"].(string); ok && strings.TrimSpace(cmd) != "" {
				return strings.TrimSpace(cmd)
			}
		}
	}
	return defaultFlueBuildCommand()
}

func readFlueManifest(deployRoot string) (flueManifest, bool, error) {
	data, err := os.ReadFile(filepath.Join(deployRoot, "dari.yml"))
	if err != nil {
		return flueManifest{}, false, err
	}
	var manifest flueManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return flueManifest{}, false, fmt.Errorf("parse dari.yml for runtime build: %w", err)
	}
	return manifest, manifestLooksLikeFlue(manifest), nil
}

func manifestLooksLikeFlue(manifest flueManifest) bool {
	switch raw := manifest.Harness.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(raw) == "flue"
	case map[string]any:
		kind, _ := raw["kind"].(string)
		return strings.TrimSpace(kind) == "flue"
	default:
		return false
	}
}

func extractArchive(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("source bundle contains unsupported entry %s", hdr.Name)
		}
		clean := filepath.Clean(filepath.FromSlash(hdr.Name))
		if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.IsAbs(clean) {
			return fmt.Errorf("source bundle contains invalid path %s", hdr.Name)
		}
		target := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(f, tr)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
}

func requirePortableDependencyStrategy(projectDir string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "package-lock.json")); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if goruntime.GOOS == "linux" && goruntime.GOARCH == "amd64" {
		return nil
	}
	return errors.New("Flue deploy prebuilds Linux/x64 runtime archives; deploying from non-Linux/x64 machines currently requires package-lock.json so npm dependencies can be reinstalled for the hosted target")
}

func injectDariPersistence(projectDir string) error {
	source := strings.Join([]string{
		"import { sqlite } from '@flue/runtime/node';",
		"const path = process.env.DARI_FLUE_SQLITE_PATH;",
		"if (!path) throw new Error('DARI_FLUE_SQLITE_PATH is required.');",
		"export default sqlite(path);",
		"",
	}, "\n")
	return os.WriteFile(filepath.Join(projectDir, "db.ts"), []byte(source), 0o644)
}

func installTargetRuntimeDependencies(projectDir string, buildOutput io.Writer) error {
	if _, err := os.Stat(filepath.Join(projectDir, "package-lock.json")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(filepath.Join(projectDir, "node_modules")); err != nil {
		return err
	}
	return runShellCommand(
		projectDir,
		"npm ci --no-audit --no-fund --os=linux --cpu=x64 --libc=glibc",
		"install Linux Flue runtime dependencies",
		buildOutput,
	)
}

func runBuildCommand(projectDir, command string, buildOutput io.Writer) error {
	return runShellCommand(projectDir, command, "build Flue runtime archive", buildOutput)
}

func runShellCommand(projectDir, command, label string, buildOutput io.Writer) error {
	home, err := os.MkdirTemp("", "dari-flue-build-home-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(home)
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = projectDir
	cmd.Env = []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
	if buildOutput == nil {
		buildOutput = io.Discard
	}
	cmd.Stdout = buildOutput
	cmd.Stderr = buildOutput
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func requireRuntimeBuild(projectDir string) error {
	if _, err := os.Stat(filepath.Join(projectDir, "dist", "server.mjs")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Flue build did not produce dist/server.mjs")
		}
		return err
	}
	if info, err := os.Stat(filepath.Join(projectDir, "node_modules")); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("Flue runtime archive build did not produce node_modules")
		}
		return err
	} else if !info.IsDir() {
		return errors.New("Flue runtime archive build produced non-directory node_modules")
	}
	return nil
}

func defaultFlueBuildCommand() string {
	return strings.Join([]string{
		"set -eu",
		"if [ ! -x node_modules/.bin/flue ]; then",
		"  if [ -f package-lock.json ]; then npm ci --no-audit --no-fund;",
		"  elif [ -f pnpm-lock.yaml ]; then corepack enable && pnpm install --frozen-lockfile;",
		"  elif [ -f bun.lockb ] || [ -f bun.lock ]; then if command -v bun >/dev/null 2>&1; then bun install --frozen-lockfile; else npm install --no-audit --no-fund; fi;",
		"  else npm install --no-audit --no-fund; fi",
		"fi",
		"./node_modules/.bin/flue build --target node",
	}, "\n")
}
