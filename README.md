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

## Coding Agents

Launch an installed coding agent through Dari:

```bash
dari --claude
dari --codex
dari --pi
```

On first launch, Dari signs you in if needed and creates and privately caches one Routing key. The first Claude or Codex launch also opens an arrow-key model checklist: up/down moves, space toggles a model, and the right arrow opens its thinking levels, where any combination can be checked. Recommended defaults are preselected and clearly marked. Codex keeps your organization's default router and its current models unless you change the selection. Claude Code gets a separate router with Claude Fable 5, Claude Opus 5, and GLM 5.3 Flash preselected. Dari applies recommended reasoning levels and managed providers to selected models; the Claude router also uses the current default public eval set.

Dari then supplies the appropriate router endpoint and `dari/routing` model without overwriting the agent's own configuration. Later launches start immediately. Set `DARI_ROUTING_API_KEY` to use an existing Routing key instead.

Every argument after the launcher flag is forwarded to the agent. This includes interactive and non-interactive modes, prompts, and agent-specific flags:

```bash
dari --claude --print "Review this diff"
dari --codex exec --sandbox workspace-write "Fix the failing tests"
dari --pi --tools read,grep,find,ls -p "Review this repository"
```

The launcher selector must be the first argument. Dari-owned provider and model settings take precedence if conflicting agent flags are passed, so the process remains routed through Dari. The native agent executable must already be installed and available on `PATH`.

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

Save BYOK values once, then reference their stable IDs in router configuration:

```bash
printf '%s' "$OPENROUTER_API_KEY" | \
  dari credentials provider add openrouter "OpenRouter Production" --value-stdin
```

```yaml
name: OpenRouter Production
enabled_models:
  - openrouter/openai/gpt-5.6-sol
provider_credential_ids:
  openrouter: cred_...
routing_strategy: slm
```

Use `dari credentials provider update` to change a saved credential without changing its ID. For AWS IAM credentials, pass only `--aws-region` to keep the stored keys, or include `--aws-access-key-id-env`, `--aws-secret-access-key-env`, and optional `--aws-session-token-env` to replace them. Run `dari router models` first to see the current catalog.

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

Create an eval scorecard from a CSV:

```csv
model_id,score,thinking_level,notes
openai/gpt-5.6-sol,87,high,Strong public run
openai/gpt-5.6-sol,82,off,Non-reasoning run
anthropic/claude-fable-5,81,,Generic score
```

```bash
dari eval create \
  --name "SWE-bench Verified" \
  --description "Public benchmark scores." \
  --min-score 0 \
  --max-score 100 \
  --file scores.csv
```

`model_id` and `score` must be the first two columns. Optional columns are `thinking_level`, `notes`, and `metadata_json` (a JSON object). Each `(model_id, thinking_level)` pair must be unique. Use `--file -` to read the CSV from standard input.

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
