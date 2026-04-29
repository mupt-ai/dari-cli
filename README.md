# Dari CLI

`dari` validates, packages, and publishes agent projects to Agent Host.

Full docs: https://docs.dari.dev

## Install

```bash
brew install mupt-ai/tap/dari
```

Or download a release archive from [Releases](https://github.com/mupt-ai/dari-cli/releases).

## Commands

Most commands require `dari auth login` first. The CLI talks to `https://api.dari.dev`.

### Headless auth (CI, scripts)

Set `DARI_API_KEY` to bypass browser login. When set, the CLI uses it as the
bearer for every request and skips cached state entirely.

```bash
export DARI_API_KEY=dari_...
```

Create a key from a logged-in shell via `dari api-keys create --name ci`.

What works under `DARI_API_KEY`:

- `dari deploy`
- `dari agent list` / `dari agent delete`
- `dari session create|get|send|events`

What does **not** work today (server currently enforces Supabase user JWT on
these routes):

- `dari auth login|logout` (by design — no login needed)
- `dari org list|create|switch|members|invite`
- `dari api-keys list|create|revoke`
- `dari credentials list|add|remove`

For those, run an interactive `dari auth login` first.

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
| `--quiet` | Suppress per-stage progress on stderr |

### api-keys

```bash
dari api-keys list
dari api-keys create --name <name>           # plaintext key returned once
dari api-keys revoke <key_id>
```

### credentials

Stored secrets referenced by name from `dari.yml`. You only need these for explicit BYOK fields (`llm.api_key_secret`, `sandbox.provider_api_key_secret`) or names listed under `sandbox.secrets`; omitted provider key fields use Dari-managed defaults.

```bash
dari credentials list
dari credentials add <name> [value]          # prompts if value omitted
dari credentials add <name> --value-stdin < secret.txt
dari credentials remove <name>
```

### agent

```bash
dari agent list                              # list deployed agents
dari agent delete <agent_id> [--yes]         # soft-delete
```

### session

```bash
dari session create --agent <agent_id>
dari session get <session_id>
dari session send <session_id> <text>        # or --stdin < message.txt
dari session events <session_id> [--limit N]
```

## Bundle shape

The repo root must contain:

- `dari.yml`
- any prompt files referenced by `instructions`
- custom tools under `tools/<name>/tool.yml`
- `Dockerfile` only if `dari.yml` sets `sandbox.dockerfile`; otherwise the default E2B base image is used.

Supported `harness` values: `pi`.

Minimal `dari.yml`:

```yaml
name: support-agent
harness: pi

instructions:
  system: prompts/system.md

sandbox:
  provider: e2b

llm:
  model: openai/gpt-5.5
```

Full schema: https://docs.dari.dev/manifest.

## Local development

```bash
go test ./...
go build ./cmd/dari
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
