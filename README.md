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
dari init my-agent
dari credentials add DARI_API_KEY
dari init recursive-agent --recursive
```

`--recursive` scaffolds an agent with the Dari CLI and a `skills/dari` playbook so it can deploy copies of itself and start child sessions. The recursive manifest references a stored runtime credential named `DARI_API_KEY`; the key is not written to `dari.yml`.

### Headless auth (CI, scripts)

Set `DARI_API_KEY` to a platform-scoped key to bypass browser login. When set,
the CLI uses it as the bearer for every request and skips cached state entirely.

```bash
export DARI_API_KEY=dari_...
```

Create a platform key for CLI/API use from a logged-in shell via
`dari api-keys create --name ci`. Add `--scope routing` for a router-traffic
key used against `https://routing.dari.dev/...`, or repeat/comma-separate
`--scope` for a dual-scope key that works for both management API calls and
router traffic.

What works under `DARI_API_KEY`:

- `dari deploy`
- `dari agent list|versions|version|status|webhook|delete`
- `dari api-keys list|create|revoke`
- `dari credentials list|add|remove`
- `dari eval list|get`
- `dari org members|invite`
- `dari router list|get|models|create|update|delete`
- `dari session list|create|get|send|events`
- `dari storage connect gcs`

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

Packages the checkout and publishes an agent version. Agent names are unique within an organization: deploying a bundle whose `dari.yml` name already exists creates a new version of that existing agent. If legacy duplicates make the name ambiguous, re-run with `--agent-id`.

| Flag | Description |
| --- | --- |
| `--api-key` | Override the cached org key |
| `--agent-id` | Publish to a specific agent instead of resolving by name |
| `--router-id` | Publish this version with a Dari Router model backend; accepts an `rtr_...` ID or copied router endpoint URL and falls back to `$DARI_ROUTER_ID` |
| `--dry-run` | Build the local bundle and print the publish flow without uploading |
| `--quiet` | Suppress per-stage progress on stderr |

### api-keys

```bash
dari api-keys list
dari api-keys create --name <name> [--scope platform|routing]
dari api-keys revoke <key_id>
```

`platform` keys authenticate CLI/management API commands. `routing` keys
authenticate router traffic such as
`curl https://routing.dari.dev/rtr_.../chat/completions`. Use
`--scope platform,routing` when one key needs both.

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

Router commands accept either an `rtr_...` ID or a copied router endpoint URL.
`router update` only changes the flags you pass; everything else keeps its
current value. Stored provider keys are write-only — pass `--provider-key-env`
(preferred) or `--provider-key` to replace one, or `--managed-key <provider>`
to switch that provider to Dari-managed billing. With `--strategy heuristic`,
pass `--performance-weight` and `--price-weight` together (they must sum
to 1), plus a repeatable `--eval-weight eval_id=WEIGHT` for every imported
eval (those must also sum to 1). Eval
scorecards themselves are still created in the dashboard; `dari eval list`
discovers IDs for `--eval`.

### eval

```bash
dari eval list
dari eval get <eval_id>
```

### credentials

Stored secrets referenced by name from `dari.yml`. You only need these for explicit BYOK fields (`llm.api_key_secret`, `sandbox.provider_api_key_secret`) or names listed under `sandbox.secrets`; omitted provider key fields use Dari-managed defaults.

```bash
dari credentials list
dari credentials add <name> [value]          # prompts if value omitted
dari credentials add <name> --value-stdin < secret.txt
dari credentials remove <name>
```

### storage

```bash
dari storage connect gcs <name> \
  --bucket <bucket> \
  --base-prefix <prefix> \
  --service-account-key ./service-account.json
```

This stores the service account JSON as a runtime credential and prints the
`sandbox.storage_binding` manifest snippet to use in `dari.yml`.

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

`<agent>` can be an agent ID or an unambiguous agent name. `agent delete` hides
the agent and stops new sessions; published versions are preserved.

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

Supported `harness.kind` values: `pi`.

Minimal direct-model `dari.yml`:

```yaml
name: support-agent
harness: pi

instructions:
  system: prompts/system.md

llm:
  model: openai/gpt-5.5
```

Router-backed agents can put the router selection in source instead of defining
an `llm` block:

```yaml
name: support-agent
harness: pi

instructions:
  system: prompts/system.md

model_backend:
  router_id: rtr_...
```

Full schema: https://docs.dari.dev/manifest.

## Local development

```bash
go test ./...
go build ./cmd/dari
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
