# Dari CLI

`dari` validates, packages, and publishes agent projects to Agent Host.

Full docs: https://docs.dari.dev

## Install

```bash
pip install dari
```

## Commands

Most commands require `dari auth login` first. The CLI talks to `https://api.dari.dev`.

### auth

```bash
dari auth login      # browser login, caches org key locally
dari auth logout     # clear local login state
dari auth status     # show current login and org
```

### org

```bash
dari org list
dari org create <name>
dari org switch <organization>               # slug or id
dari org members
dari org invite <email> [--role owner|admin|member]   # default: member
```

### deploy

```bash
dari deploy [repo_root]
```

Packages the checkout and publishes a new agent version.

| Flag | Description |
| --- | --- |
| `--api-key` | Override the cached org key |
| `--agent-id` | Publish to a specific agent instead of resolving by name |
| `--dry-run` | Validate and package without uploading |

### api-keys

```bash
dari api-keys list
dari api-keys create --name <name>           # plaintext key returned once
dari api-keys revoke <key_id>
```

### credentials

Stored secrets referenced by name from `dari.yml` (e.g. `llm.api_key_secret: OPENROUTER_API_KEY`).

```bash
dari credentials list
dari credentials add <name> [value]          # prompts if value omitted
dari credentials add <name> --value-stdin < secret.txt
dari credentials remove <name>
```

### manifest

```bash
dari manifest validate [repo_root]
dari manifest validate --json                # prints normalized manifest
```

## Bundle shape

The repo root must contain:

- `dari.yml`
- any prompt files referenced by `instructions`
- custom tools under `tools/<name>/tool.yml`
- `Dockerfile` only if `dari.yml` sets a `runtime:` block; otherwise the default E2B base image is used.

Supported `harness` values: `pi`.

Minimal `dari.yml`:

```yaml
name: support-agent
harness: pi

instructions:
  system: prompts/system.md

sandbox:
  provider: e2b
  provider_api_key_secret: E2B_API_KEY

llm:
  model: anthropic/claude-sonnet-4.6
  base_url: https://openrouter.ai/api/v1
  api_key_secret: OPENROUTER_API_KEY
```

Full schema: https://docs.dari.dev/manifest.

## Local development

```bash
uv sync --group dev
uv run pytest
```

## Release

1. Bump `version` in `pyproject.toml`.
2. Refresh `uv.lock` so the editable entry matches.
3. Commit the bump before tagging.
4. `uv build && uv run pytest`.
5. Push a `v0.1.x` tag matching `pyproject.toml` — the release workflow rejects mismatched tags.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
