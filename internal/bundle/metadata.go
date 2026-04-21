package bundle

import (
	"os"
	"strings"
)

// Metadata captures best-effort git/CI provenance. Values are either string
// or bool; nil keys are omitted when the bundle is marshaled.
type Metadata map[string]any

// CollectMetadata returns origin, git commit/ref/dirty, and (in GitHub
// Actions) CI provider + run URL. Matches collect_source_bundle_metadata in
// the Python CLI.
func CollectMetadata(deployRoot string) Metadata {
	env := envReader(os.Environ())
	m := Metadata{"origin": detectOrigin(env)}

	git := collectGitMetadata(deployRoot)
	if git.commitSHA != "" {
		m["git_commit_sha"] = git.commitSHA
	} else if v := env["GITHUB_SHA"]; v != "" {
		m["git_commit_sha"] = v
	}
	if git.ref != "" {
		m["git_ref"] = git.ref
	} else if v := env["GITHUB_REF"]; v != "" {
		m["git_ref"] = v
	}
	if git.dirtyKnown {
		m["git_dirty"] = git.dirty
	}

	if env["GITHUB_ACTIONS"] == "true" {
		m["ci_provider"] = "github_actions"
		if v := env["GITHUB_RUN_ID"]; v != "" {
			m["github_run_id"] = v
			server := strings.TrimRight(env["GITHUB_SERVER_URL"], "/")
			repo := env["GITHUB_REPOSITORY"]
			if server != "" && repo != "" {
				m["ci_run_url"] = server + "/" + repo + "/actions/runs/" + v
			}
		}
	}
	return m
}

type gitMetadata struct {
	commitSHA  string
	ref        string
	dirty      bool
	dirtyKnown bool
}

func collectGitMetadata(deployRoot string) gitMetadata {
	repoRoot, ok := runGit(deployRoot, []string{"rev-parse", "--show-toplevel"})
	if !ok {
		return gitMetadata{}
	}
	commit, _ := runGit(repoRoot, []string{"rev-parse", "HEAD"})
	branch, _ := runGit(repoRoot, []string{"symbolic-ref", "--quiet", "--short", "HEAD"})

	dirtyArgs := []string{"status", "--porcelain", "--untracked-files=normal", "-z"}
	dirtyOut, _ := runGitBytes(repoRoot, dirtyArgs)

	return gitMetadata{
		commitSHA:  strings.TrimSpace(commit),
		ref:        strings.TrimSpace(branch),
		dirty:      len(dirtyOut) > 0,
		dirtyKnown: true,
	}
}

func detectOrigin(env map[string]string) string {
	if env["GITHUB_ACTIONS"] == "true" || env["CI"] == "true" {
		return "ci"
	}
	return "local_cli"
}

func envReader(pairs []string) map[string]string {
	m := make(map[string]string, len(pairs))
	for _, p := range pairs {
		i := strings.IndexByte(p, '=')
		if i <= 0 {
			continue
		}
		m[p[:i]] = p[i+1:]
	}
	return m
}
