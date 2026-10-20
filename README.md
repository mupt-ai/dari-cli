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
dari claude
dari codex
dari opencode
dari pi
```

On first launch, Dari signs you in if needed and creates and privately caches one Routing key. Subscription setup runs even if you already used `dari auth login` or have a cached router. Codex asks to connect your ChatGPT subscription, Claude asks to connect your Claude Code subscription, and OpenCode and Pi offer either provider or Dari managed billing. Explicit opt-outs are remembered per user and launcher. If it is not connected to Dari yet, the launcher opens the provider login and finishes the connection before continuing; later launches reuse it automatically. Personal subscriptions require the organization's Dari Pro plan, separate from the provider subscription. If Pro is inactive, onboarding lets you review plans in the browser, recheck activation, or explicitly choose Dari managed billing. It never purchases a plan for you.

You can connect more than one subscription per provider, such as two Claude Max accounts. Routing uses one until it reaches its usage limit, holds it until its reset, and then stays on the next one in connection order. `dari credentials subscription list` shows which is active and which are held; `dari credentials subscription connect <claude-code|codex>` connects another (`--reconnect NAME` replaces one), `rename <claude-code|codex|NAME> <label>` gives one your own name, and `mark-available` / `disconnect` take a provider alias or, when several are connected, the subscription's name. `dari credentials subscription auto-reset <codex|NAME> on` lets Dari spend that ChatGPT account's banked Codex resets once every ChatGPT subscription is exhausted (off by default).

The first Claude or Codex launch also opens an arrow-key model checklist: up/down moves, space toggles a model, and the right arrow opens its thinking levels, where any combination can be checked. Recommended defaults are preselected and clearly marked. Codex and Claude preselect their named model presets from the same backend configuration used by the web app, including reasoning levels. Codex applies confirmed selections to your organization's default router; Claude Code gets a separate router using the Claude preset's models. Dari automatically picks relevant evals for each request from your organization's evals and Public Evals. The web app's Default preset also supplies new organizations' initial router models. Saved launcher selections are reused on later launches. Dari applies recommended reasoning levels and managed providers to selected models.

Dari then supplies the appropriate router endpoint and `dari/routing` model without overwriting the agent's own configuration. Later launches reuse setup while checking for a connected subscription. Set `DARI_ROUTING_API_KEY` to use an existing Routing key instead. Pi's footer, Claude Code's status line, Codex's footer, and OpenCode's prompt footer show the model serving each turn, its thinking level, and the turns left on the current routing lease. All four use the same status formatter.

Codex's footer requires Codex 0.162.1 or newer. Interactive launches use a session-local bridge that reads Dari response headers and updates only the UI's thread-name display; saved session names, resume lists, and routing requests are unchanged. This also affects other thread-name displays in the active UI, such as its terminal title. The bridge uses loopback-only listeners, requires the routing bearer key for HTTP and a separate capability for UI connections, streams responses without buffering, and stops with the launcher. UI reconnects preserve the local app server and active turns. Codex prints its standard remote-disconnect message on exit; its suggested direct reconnect URL expires when Dari exits, so resume with `dari codex resume` instead. Closing the launcher stops its local app server; it does not leave work running in a shared Codex daemon.

Set `DARI_CODEX_STATUS=0` to use Codex without the bridge and keep your existing footer layout. Explicit `--remote` or `--profile`/`-p` launches and non-interactive commands such as `exec` and `review` also keep their original execution path. Dari's interactive footer layout is session-scoped and does not rewrite your Codex configuration.

OpenCode receives its Dari provider, `dari/routing` model, and status plugin through `OPENCODE_CONFIG_CONTENT`, which replaces any value you set for that variable; files under `~/.config/opencode` and project config are unchanged. Interactive, `run`, `mini`, `api`, `models`, and `stats` launches add `--standalone` so a private server picks up that configuration instead of OpenCode's shared background service; `--server` and `pair` are rejected for the same reason (use `dari opencode serve` for other clients). Management commands such as `service`, `session`, and `auth` run without Dari's configuration so OpenCode's long-lived background service never picks it up. Built-in agents, subagents, and title, summary, and compaction tasks are pinned to `dari/routing`.

Every argument after the launcher subcommand is forwarded to the agent. This includes interactive and non-interactive modes, prompts, and agent-specific flags:

```bash
dari claude --print "Review this diff"
dari codex exec --sandbox workspace-write "Fix the failing tests"
dari opencode run "Explain this repository"
dari pi --tools read,grep,find,ls -p "Review this repository"
```

The launcher subcommand must be the first argument. Dari-owned provider and model settings take precedence if conflicting agent flags are passed, so the process remains routed through Dari. The native agent executable must already be installed and available on `PATH`, or set `DARI_<AGENT>_PATH` (for example `DARI_OPENCODE_PATH`) to its location.

## Common Workflows

Create a router from flags:

```bash
dari router create "Production Router" \
  --model openai/gpt-5.6-sol \
  --model anthropic/claude-sonnet-5 \
  --managed-key openai \
  --managed-key anthropic \
  --strategy dari
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
routing_strategy: dari
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
routing_strategy: dari
```

Use `dari credentials provider update` to change a saved credential without changing its ID. For AWS IAM credentials, pass only `--aws-region` to keep the stored keys, or include `--aws-access-key-id-env`, `--aws-secret-access-key-env`, and optional `--aws-session-token-env` to replace them. Run `dari router models` first to see the current catalog. `dari router request-costs <request_uuid>...` looks up content-free model reference costs for requests made by the `DARI_ROUTING_API_KEY`; set that environment variable to the key used for inference. Results are scoped to the key, may be delayed while telemetry lands, and are not a subscription cash charge.

Inspect and manage routers:

```bash
dari router list
dari router get <router_id_or_endpoint>
dari router update <router_id_or_endpoint> --model openai/gpt-5.6-sol
dari router delete <router_id_or_endpoint> --yes
```

Dari automatically picks the relevant evals for each request from your organization's evals and Public Evals. Use `dari eval` to manage your organization's eval scorecards; router configuration no longer selects them. Eval score imputation estimates a missing eval score for a model and thinking level from related measured scores; measured scores are never changed. It is on by default for new routers. Pass `--eval-score-imputation=false` to `dari router create` or `dari router update`, or set `eval_score_imputation: false` in `router.yml`, to turn it off; omitting the flag on update keeps the router's current setting.

Inspect activity and evals:

```bash
dari activity filter-options --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity overview --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity models --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity people --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity conversations --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity conversations get rreq_... --request-class main
dari activity conversations get rreq_... --request-class subagent --agent-id agent_...
dari activity tools list --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari activity skills list --from 2026-07-01T00:00:00Z --to 2026-07-08T00:00:00Z
dari eval list
dari eval list --model anthropic/claude-fable-5:medium,high --model openai/gpt-5.6-sol
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
