# Dari CLI

`dari` packages and publishes Flue projects to Dari.

Full docs: https://docs.dari.dev

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | bash
```

This installs the native macOS/Linux binary for your CPU. To choose a destination, set `DARI_INSTALL_DIR`, for example:

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | DARI_INSTALL_DIR="$HOME/bin" bash
```

Or download a native release archive from [Releases](https://github.com/mupt-ai/dari-cli/releases).

Update later with:

```bash
dari update
```

The CLI also checks for newer releases periodically and prints a stderr notice when your installed version is behind. Set `DARI_DISABLE_UPDATE_CHECK=1` to disable that check.

## Commands

Most commands require `dari auth login` first. The CLI talks to `https://api.dari.dev`.

### init

```bash
dari init chat
```

Creates a normal Flue project with `package.json`, `agents/chat.ts`, and a small Dari deploy file. The generated OpenRouter example declares the provider key it reads at runtime:

```yaml
name: chat
sandbox:
  secrets:
    - OPENROUTER_API_KEY
```

### Headless auth (CI, scripts)

Set `DARI_API_KEY` to a platform-scoped key to bypass browser login. When set, the CLI uses it as the bearer for every request and skips cached state entirely.

```bash
export DARI_API_KEY=dari_...
```

Create a platform key for CLI/API use from a logged-in shell via `dari api-keys create --name ci`. Add `--scope routing` for a router-traffic key used against `https://routing.dari.dev/...`, or repeat/comma-separate `--scope` for a dual-scope key that works for both management API calls and router traffic.

What works under `DARI_API_KEY`:

- `dari deploy`
- `dari agent list|versions|version|status|webhook|delete`
- `dari api-keys list|create|revoke`
- `dari credentials list|add|remove`
- `dari eval list|get`
- `dari org members|invite`
- `dari router list|get|models|create|update|delete`
- `dari session list|create|get|send|events`

What does **not** work under `DARI_API_KEY`:

- `dari auth login|logout` (by design — no login needed)
- `dari org list|create|switch|delete` (these operate on the browser-login org list rather than the API key's current org)

### update

```bash
dari update           # install the latest release
dari update --check   # report whether an update is available
```

Native installs replace the current binary after verifying the release checksum.

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
dari org invite <email> [--role owner|admin|member]   # emails an invite; default: member
```

### deploy

```bash
dari deploy [repo_root]
```

Packages the checkout and publishes an agent version. For Flue projects with `package.json`, live deploy installs dependencies and runs the Flue build locally once, then uploads a prebuilt runtime archive so hosted message workers only extract and run it. From non-Linux/x64 machines, commit `package-lock.json` so the CLI can reinstall runtime dependencies for the hosted Linux target before upload. Agent names are unique within an organization: deploying a bundle whose `dari.yml` name already exists creates a new version of that existing agent. If legacy duplicates make the name ambiguous, re-run with `--agent-id`.

| Flag | Description |
| --- | --- |
| `--api-key` | Override the cached org key |
| `--agent-id` | Publish to a specific agent instead of resolving by name |
| `--dry-run` | Package the source bundle and print the publish flow without installing dependencies, building, or uploading |
| `--quiet` | Suppress per-stage progress on stderr |

### api-keys

```bash
dari api-keys list
dari api-keys create --name <name> [--scope platform|routing]
dari api-keys revoke <key_id>
```

`platform` keys authenticate CLI/management API commands. `routing` keys authenticate router traffic such as `curl https://routing.dari.dev/rtr_.../chat/completions`. Use `--scope platform,routing` when one key needs both.

### router

```bash
dari router list
dari router get <router_id_or_endpoint>
dari router models                           # model catalog grouped by provider
dari router create <name> --model <model_id> [--model <model_id> ...] \
  [--provider-key provider=KEY | --provider-key-env provider=ENV_VAR | --managed-key provider] \
  [--eval <eval_id> ...] \
  [--strategy slm|heuristic] \
  [--performance-weight 0.7 --price-weight 0.3 --eval-weight <eval_id>=1.0]
dari router update <router_id_or_endpoint> [--name <name>] [--model ...] \
  [--provider-key ...] [--managed-key ...] [--eval ...] [--clear-evals] \
  [--strategy ...] [--performance-weight ... --price-weight ... --eval-weight ...]
dari router delete <router_id_or_endpoint> [--yes]
```

Router commands accept either an `rtr_...` ID or a copied router endpoint URL. `router update` only changes the flags you pass; everything else keeps its current value. Stored provider keys are write-only — pass `--provider-key-env` (preferred) or `--provider-key` to replace one, or `--managed-key <provider>` to switch that provider to Dari-managed billing.

### eval

```bash
dari eval list
dari eval get <eval_id>
```

### credentials

Stored credentials are named secrets for values your Flue project needs at runtime, such as a provider key for a model API.

```bash
dari credentials list
dari credentials add OPENROUTER_API_KEY      # prompts if value omitted
dari credentials add <name> --value-stdin < secret.txt
dari credentials remove <name>
```

### agent

```bash
dari agent list
dari agent versions <agent>
dari agent version show <agent> <version_id>
dari agent version files <agent> <version_id>
dari agent version cat <agent> <version_id> <path>
dari agent status [repo_root] [--agent-id <agent>]
dari agent webhook get <agent>
dari agent webhook set <agent> <webhook_url> [--event <event_type> ...]
dari agent webhook clear <agent>
dari agent webhook rotate-secret <agent>
dari agent delete <agent> [--yes]
```

`<agent>` can be an agent ID or an unambiguous agent name. `agent delete` hides the agent and stops new sessions; published versions are preserved.

### session

```bash
dari session list --agent <agent_id>
dari session create --agent <agent_id>
dari session create --agent <agent_id> --secret-env INTERNAL_API_TOKEN
dari session create --agent <agent_id> --internet-access
dari session get <session_id>
dari session send <session_id> <text>        # or --stdin < message.txt
dari session send --agent <agent_id> <text>  # creates a new session first
dari session events <session_id> [--limit N]
```

## Bundle Shape

The repo root must contain `dari.yml` and a Flue project with `agents/<name>.ts`.

Minimal `dari.yml`:

```yaml
name: chat
```

If your Flue code reads runtime secrets, declare their names under `sandbox.secrets` and store them with `dari credentials add` before deploy. Prompts, models, tools, and agent behavior live in Flue code.

## Local Development

```bash
go test ./...
go build ./cmd/dari
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
