# Dari CLI

`dari` is the command-line client for creating and managing Dari routers, credentials, API keys, organizations, and evals.

Full documentation: <https://docs.dari.dev>

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | bash
```

To choose another destination:

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | DARI_INSTALL_DIR="$HOME/bin" bash
```

You can also download a native archive from [Releases](https://github.com/mupt-ai/dari-cli/releases). Update an installed binary with `dari update`. Set `DARI_DISABLE_UPDATE_CHECK=1` to disable update notices.

## Sign In

Interactive commands use browser authentication:

```bash
dari auth login
dari auth status
```

For CI and scripts, set `DARI_API_KEY` to a Management key. It is used as the bearer credential and bypasses cached login state:

```bash
export DARI_API_KEY="dari_..."
dari router list
```

Create Management and Routing keys with `dari api-keys create`. Management keys authenticate CLI and management API operations; Routing keys authenticate requests sent to router endpoints. Keys are shown once, so store them in a secret manager.

## Common Workflows

Create a router from flags:

```bash
dari router create "Production Router" \
  --model openai/gpt-5.6-sol \
  --model anthropic/claude-sonnet-5 \
  --managed-key openai \
  --managed-key anthropic \
  --strategy slm
```

Or create one from `router.yml`:

```yaml
name: Production Router
enabled_models:
  - openai/gpt-5.6-sol
  - anthropic/claude-sonnet-5
provider_key_sources:
  openai: managed
  anthropic: managed
routing_strategy: slm
```

```bash
dari router create ./router.yml
```

Keep BYOK values out of YAML by using `provider_key_envs`. Run `dari router models` first to see the current catalog.

Inspect and manage routers:

```bash
dari router list
dari router get <router_id_or_endpoint>
dari router update <router_id_or_endpoint> --model openai/gpt-5.6-sol
dari router delete <router_id_or_endpoint> --yes
```

Inspect activity and evals:

```bash
dari activity filter-options --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity overview --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity models --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity people --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity conversations --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity tools list --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity skills list --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari eval list
dari eval get <eval_id>
```

## Agent Skill

Print managed-router instructions for a coding agent:

```bash
dari --skill
```

The command prints the managed-router workflow directly so an agent can load and follow it.

## Command Discovery

Run `dari --help` or `<command> --help` for the complete, version-specific command and flag reference. The [Dari documentation](https://docs.dari.dev) includes the managed router guides, API reference, public manifests, and coding-agent integrations.

## Development

```bash
go test ./...
go build ./cmd/dari
```

See [CONTRIBUTING.md](./CONTRIBUTING.md) for contributor guidance.
