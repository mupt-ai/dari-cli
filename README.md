# Dari CLI

`dari` packages and publishes agent projects to Dari.

Full docs: https://docs.dari.dev

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | bash
```

This installs the native macOS/Linux binary for your CPU. To choose a destination, set `DARI_INSTALL_DIR`, for example:

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | DARI_INSTALL_DIR="$HOME/bin" bash
```

Homebrew is also supported:

```bash
brew install mupt-ai/tap/dari
```

Or download a release archive from [Releases](https://github.com/mupt-ai/dari-cli/releases).

Update later with:

```bash
dari update
```

The CLI also checks for newer releases periodically and prints a stderr notice when your installed version is behind. Set `DARI_DISABLE_UPDATE_CHECK=1` to disable that check.

## Commands

Most commands require `dari auth login` first. The CLI talks to `https://api.dari.dev`.

### init

```bash
dari init my-agent
dari credentials add DARI_API_KEY
dari init recursive-agent --recursive
```

`--recursive` scaffolds an agent with the Dari CLI and a `skills/dari` playbook so it can deploy copies of itself and start child sessions. The recursive manifest references a stored runtime credential named `DARI_API_KEY`; the key is not written to `dari.yml`.

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
- `dari session list|create|get|send|events`

What does **not** work today (server currently enforces Supabase user JWT on
these routes):

- `dari auth login|logout` (by design — no login needed)
- `dari org list|create|switch|members|invite`
- `dari api-keys list|create|revoke`
- `dari credentials list|add|remove`

For those, run an interactive `dari auth login` first.

### update

```bash
dari update           # install the latest release
dari update --check   # report whether an update is available
```

Homebrew-managed installs are upgraded through `brew update` and `brew upgrade dari`; release-archive installs replace the current binary after verifying the release checksum.

### auth

```bash
dari auth login      # opens the Dari web login page, caches org key locally; paste callback URL if redirect fails
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

Packages the checkout and publishes an agent version. Agent names are unique within an organization: deploying a bundle whose `dari.yml` name already exists creates a new version of that existing agent. If legacy duplicates make the name ambiguous, re-run with `--agent-id`.

| Flag | Description |
| --- | --- |
| `--api-key` | Override the cached org key |
| `--agent-id` | Publish to a specific agent instead of resolving by name |
| `--dry-run` | Build the local bundle and print the publish flow without uploading |
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
dari session list --agent <agent_id>
dari session create --agent <agent_id>
dari session create --agent <agent_id> --secret-env INTERNAL_API_TOKEN
dari session create --agent <agent_id> --llm claude
dari session create --agent <agent_id> --llm-api-key-env OPENROUTER_API_KEY
dari session create --agent <agent_id> --internet-access
dari session get <session_id>
dari session send <session_id> <text>        # or --stdin < message.txt
dari session send --agent <agent_id> <text>  # creates a new session first
dari session events <session_id> [--limit N]
```

`--secret NAME=VALUE` and `--secret-env NAME` pass session-scoped secrets to
the sandbox for names declared in `sandbox.secrets`. `--llm` selects a named
`llm.options` entry from the deployed manifest. `--llm-api-key` and
`--llm-api-key-env` override the LLM provider key for the session only; that
key is not exposed to sandbox code. `--internet-access` and
`--no-internet-access` override whether the execution sandbox can reach the
public internet for the session.

## Bundle shape

The repo root must contain:

- `dari.yml`
- any prompt files referenced by `instructions`
- code-first TypeScript tools as `tools/<name>/tool.ts`
- `Dockerfile` only if `dari.yml` sets `sandbox.dockerfile`; otherwise the default E2B base image is used.

Supported `harness` values: `pi`.

Minimal `dari.yml`:

```yaml
name: support-agent
harness: pi

instructions:
  system: prompts/system.md

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
