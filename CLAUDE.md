# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`dari` is the public CLI for Dari (https://dari.dev). It packages and publishes
agent bundles to `https://api.dari.dev`. End-user docs live at https://docs.dari.dev.

**This directory is public.** On every push to `main` of the monorepo, it is
force-pushed to `mupt-ai/dari-cli` via a subtree split. Do not reference
internal paths, backend internals, or secrets here.

## Commands

```bash
go vet ./...
go test -race ./...            # matches the CI `dari-cli` job
go build ./cmd/dari            # entry point: cmd/dari/main.go
go test ./internal/bundle/...  # run a single package
```

CI runs `go vet`, `go test -race`, and `go build` against `go.mod`'s pinned
toolchain (see `go.mod` for the exact Go version).

## Layout

- `cmd/dari/main.go` — entry point; wires Cobra and calls `internal/cli`.
- `internal/cli/` — one file per top-level command group (`auth`, `org`,
  `deploy`, `agent`, `session`, `apikeys`, `credentials`, `init`). `root.go`
  composes them.
- `internal/api/` — typed HTTP client for `api.dari.dev`.
- `internal/auth/` — browser-login flow, `DARI_API_KEY` bypass, cached org
  key.
- `internal/bundle/` — repo-root packaging and deterministic archive selection.
  Keep bundle behavior aligned with `dari-docs/manifest.mdx` in the monorepo.
- `internal/deploy/` — publish pipeline (source snapshot upload + finalize).
- `internal/state/` — on-disk cache for login/org selection.
- `internal/scaffold/` — `dari init` project templates.

## Auth surface

`DARI_API_KEY` bypasses browser login and is used as the bearer for every
request; cached state is skipped entirely. The server still enforces Supabase
user JWT on a subset of routes — see `README.md` for the current split of what
works under an API key vs. what requires interactive login. When adding a new
command, decide which auth mode it needs and match the existing pattern in
`internal/cli/`.

## Release

Releases are built with GoReleaser (`.goreleaser.yaml`) and distributed via
the native install script, the `mupt-ai/tap` Homebrew tap, and GitHub Releases.
Don't hand-edit version strings; the release workflow owns them.
