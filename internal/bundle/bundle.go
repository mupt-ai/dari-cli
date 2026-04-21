// Package bundle builds the deterministic tar.gz source archive that
// `dari deploy` uploads. File selection honors .gitignore when the deploy
// root is inside a git repo, and falls back to a pruned directory walk.
package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	excludedFallbackDirs = map[string]struct{}{
		".dari":         {},
		".git":          {},
		".pytest_cache": {},
		".venv":         {},
		"__pycache__":   {},
		"node_modules":  {},
	}
	excludedFallbackFiles = map[string]struct{}{
		".DS_Store": {},
	}
)

// Archive is a built source bundle.
type Archive struct {
	Content       []byte
	SHA256        string
	IncludedPaths []string
}

// FileCount returns the number of regular files in the archive.
func (a *Archive) FileCount() int { return len(a.IncludedPaths) }

// SizeBytes returns the archive byte size.
func (a *Archive) SizeBytes() int { return len(a.Content) }

// Build returns a deterministic tar.gz for deployRoot. The deploy root must
// exist, must be a directory, and must contain a top-level dari.yml.
func Build(deployRoot string) (*Archive, error) {
	resolved, err := filepath.Abs(deployRoot)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", deployRoot)
	}

	paths, err := selectPaths(resolved)
	if err != nil {
		return nil, err
	}
	if !contains(paths, "dari.yml") {
		return nil, errors.New("deploy root must contain a top-level dari.yml file")
	}

	var buf bytes.Buffer
	gzw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// Zero the gzip metadata so archives are byte-stable across runs.
	gzw.ModTime = time.Time{}
	gzw.Name = ""
	gzw.Comment = ""
	gzw.OS = 0xff // unknown — avoids os-specific byte

	tw := tar.NewWriter(gzw)
	for _, rel := range paths {
		if err := addFile(tw, resolved, rel); err != nil {
			_ = tw.Close()
			_ = gzw.Close()
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gzw.Close(); err != nil {
		return nil, err
	}

	content := buf.Bytes()
	sum := sha256.Sum256(content)
	return &Archive{
		Content:       content,
		SHA256:        hex.EncodeToString(sum[:]),
		IncludedPaths: paths,
	}, nil
}

func selectPaths(root string) ([]string, error) {
	if paths, ok, err := gitSelectedPaths(root); err != nil {
		return nil, err
	} else if ok {
		return paths, nil
	}
	return fallbackSelectedPaths(root)
}

// gitSelectedPaths uses `git ls-files` so ignored/unindexed files aren't
// included. Returns (paths, true) when root is inside a git repo.
func gitSelectedPaths(root string) ([]string, bool, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, false, nil
	}

	topText, ok := runGit(root, []string{"rev-parse", "--show-toplevel"})
	if !ok {
		return nil, false, nil
	}
	repoRoot, err := filepath.Abs(strings.TrimSpace(topText))
	if err != nil {
		return nil, false, err
	}
	prefix, err := filepath.Rel(repoRoot, root)
	if err != nil {
		return nil, false, err
	}
	// Normalize to forward-slash and empty for "same dir".
	prefix = filepath.ToSlash(prefix)
	if prefix == "." {
		prefix = ""
	}

	args := []string{"ls-files", "--cached", "--others", "--exclude-standard", "-z"}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	raw, err := runGitBytes(repoRoot, args)
	if err != nil {
		return nil, false, err
	}

	var selected []string
	for _, part := range bytes.Split(raw, []byte{0}) {
		if len(part) == 0 {
			continue
		}
		p := string(part)
		if prefix != "" {
			trimmed, ok := trimPrefix(p, prefix+"/")
			if !ok {
				continue
			}
			p = trimmed
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := ensureAllowedSnapshotPath(full, p); err != nil {
			return nil, false, err
		}
		selected = append(selected, p)
	}
	sort.Strings(selected)
	return selected, true, nil
}

func fallbackSelectedPaths(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if _, ok := excludedFallbackDirs[name]; ok {
				return fs.SkipDir
			}
			return nil
		}
		if _, ok := excludedFallbackFiles[name]; ok {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := ensureAllowedSnapshotPath(path, rel); err != nil {
			return err
		}
		paths = append(paths, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func addFile(tw *tar.Writer, root, rel string) error {
	full := filepath.Join(root, filepath.FromSlash(rel))
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source bundle cannot include symlink %s", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source bundle can only include regular files, found %s", rel)
	}
	mode := int64(0o644)
	if isExecutable(info.Mode()) {
		mode = 0o755
	}
	hdr := &tar.Header{
		Name:     rel,
		Typeflag: tar.TypeReg,
		Mode:     mode,
		Size:     info.Size(),
		ModTime:  time.Unix(0, 0),
		Format:   tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	f, err := os.Open(full)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(tw, f); err != nil {
		return err
	}
	return nil
}

func ensureAllowedSnapshotPath(full, rel string) error {
	if filepath.IsAbs(rel) {
		return fmt.Errorf("invalid source bundle path %s", rel)
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." {
			return fmt.Errorf("invalid source bundle path %s", rel)
		}
	}
	info, err := os.Lstat(full)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("source bundle cannot include symlink %s", rel)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source bundle can only include regular files, found %s", rel)
	}
	return nil
}

func isExecutable(mode fs.FileMode) bool {
	return mode&0o111 != 0
}

func runGit(cwd string, args []string) (string, bool) {
	out, err := runGitBytes(cwd, args)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

func runGitBytes(cwd string, args []string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func contains(slice []string, value string) bool {
	for _, s := range slice {
		if s == value {
			return true
		}
	}
	return false
}

func trimPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
