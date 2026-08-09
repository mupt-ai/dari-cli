# Dari CLI

`dari` manages Dari routers, credentials, API keys, and organizations from the terminal.

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

### Headless auth (CI, scripts)

Set `DARI_API_KEY` to a Management key to bypass browser login. When set, the CLI uses it as the bearer for every request and skips cached state entirely.

```bash
export DARI_API_KEY=dari_...
```

Create a Management key for CLI/API use from a logged-in shell via `dari api-keys create --name ci`. Create a separate Routing key for traffic to `https://routing.dari.dev/...` with `dari api-keys create --name router-client --type routing`.

What works under `DARI_API_KEY`:

- `dari api-keys list|create|revoke`
- `dari credentials list|add|remove`
- `dari eval list|get`
- `dari activity models`
- `dari org members|invite`
- `dari router list|get|models|create|update|delete`

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

### api-keys

```bash
dari api-keys list
dari api-keys create --name <name> [--type management|routing]
dari api-keys revoke <key_id>
```

Management keys authenticate CLI and management API commands. Routing keys authenticate router traffic such as `curl https://routing.dari.dev/rtr_.../chat/completions`. Use separate keys when a backend needs both surfaces.

### router

```bash
dari router list
dari router get <router_id_or_endpoint>
dari router models                           # model catalog grouped by provider
dari router create <name> --model <model_id> [--model <model_id> ...] \
  [--provider-key provider=KEY | --provider-key-env provider=ENV_VAR | --managed-key provider] \
  [--eval <eval_id> ...] [--strategy slm]
dari router create ./router.yml              # or a directory containing router.yml/router.yaml
dari router create --from-file ./router.yml  # same, via explicit flag (-f)
dari router update <router_id_or_endpoint> [--name <name>] [--model ...] \
  [--provider-key ...] [--managed-key ...] [--eval ...] [--clear-evals] \
  [--strategy slm]
dari router delete <router_id_or_endpoint> [--yes]
```

Router commands accept either an `rtr_...` ID or a copied router endpoint URL. `router update` only changes the flags you pass; everything else keeps its current value. Stored provider keys are write-only — pass `--provider-key-env` (preferred) or `--provider-key` to replace one, or `--managed-key <provider>` to switch that provider to Dari-managed billing.

You can keep router configuration in a local YAML file and create it with `dari router create ./router.yml`, `dari router create ./router-dir`, or explicitly with `--from-file`/`-f`. Directory inputs must contain `router.yml` or `router.yaml`. A positional argument is treated as a manifest path when it contains a path separator or ends in `.yml`/`.yaml`; anything else is treated as a router name. For BYOK providers, prefer `provider_key_envs` so secrets stay in local environment variables instead of committed files:

```yaml
name: Production Router
enabled_models:
  - openai/gpt-5.5
  - baseten/moonshotai/Kimi-K2.7-Code
provider_key_sources:
  openai: managed
  baseten: user
provider_key_envs:
  baseten: BASETEN_API_KEY
routing_strategy: slm
eval_ids: []
```

Describe Your Router rules use the `custom` strategy in YAML manifests:

```yaml
name: Custom Rules Router
enabled_models:
  - openai/gpt-5.5
  - openai/gpt-4.1-mini
model_thinking_levels:
  openai/gpt-5.5: [low, medium, high]
  openai/gpt-4.1-mini: [off]
fast_models:
  - openai/gpt-5.5
provider_key_sources:
  openai: managed
routing_strategy: custom
custom_config:
  rules:
    - when: planning and architecture
      use: openai/gpt-5.5
      thinking_level: high
    - when: implementation and refactors
      use: openai/gpt-4.1-mini
      thinking_level: null
  default: openai/gpt-4.1-mini
  default_thinking_level: null
```

`model_thinking_levels` enables exact model/thinking-level pairs and must list every enabled model when set. `fast_models` enables Fast Mode for the listed models and every entry must also appear in `enabled_models`; the API rejects models whose catalog entry does not support Fast Mode. A custom rule or fallback can pin one enabled `thinking_level`; use `null` or omit the field for Auto, which lets the router select among that model's enabled levels.

### activity

```bash
dari activity models \
  --from 2026-07-01T00:00:00Z \
  --to 2026-07-08T00:00:00Z \
  [--router-id <router_id>] \
  [--api-key-id <api_key_id> ...] [--user-id <user_id> ...] \
  [--model <model_id> ...] [--provider <provider>] \
  [--status completed|provider_error|selector_error|aborted]
```

Returns JSON for the current organization's model usage, token and cache buckets, provider spend and pricing coverage, cost per step, latency p95, completion outcomes, and observed model/provider transitions. The current user or Management API key must have organization owner or admin access. See [Model Activity](https://docs.dari.dev/router/model-activity) for API usage and metric definitions.

### eval

```bash
dari eval list
dari eval get <eval_id>
```

### credentials

Stored credentials are named secrets for the current organization, such as a provider key for a model API.

```bash
dari credentials list
dari credentials add OPENROUTER_API_KEY      # prompts if value omitted
dari credentials add <name> --value-stdin < secret.txt
dari credentials remove <name>
```

## Local Development

```bash
go test ./...
go build ./cmd/dari
```

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).
