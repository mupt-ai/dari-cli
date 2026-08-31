---
name: dari
description: Use when the user wants to create, configure, inspect, or call a Dari managed router; work with router.yml/router.yaml manifests, the Dari CLI/API, provider keys, evals, or routing API keys.
---

# Dari Managed Routers

Dari's managed path lets you define a router in YAML and create it with the public CLI. The self-hosted framework path is `@mupt-ai/dari-router`; see its [public documentation](https://docs.dari.dev/framework/overview) for framework usage.

## Create A Router

Install the CLI and authenticate:

```bash
curl -fsSL https://raw.githubusercontent.com/mupt-ai/dari-cli/main/install.sh | bash
dari auth login
dari router models
```

Create a `router.yml`:

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

Create it:

```bash
dari router create ./router.yml
```

The command accepts a file or a directory containing `router.yml` or `router.yaml`. The CLI strictly parses and validates the manifest before sending it to the managed API.

Models default to the provider shown by `dari router models`; the CLI resolves those defaults from the model catalog during create. Bind a model to a different provider with `model_providers` (when present, it must cover every enabled model):

```yaml
enabled_models:
  - openai/gpt-5.6-sol
  - zai-org/GLM-5.3
model_providers:
  openai/gpt-5.6-sol: openai
  zai-org/GLM-5.3: fireworks
```

## Provider Keys

Use Dari-managed keys when available:

```yaml
provider_key_sources:
  openai: managed
```

For BYOK, read the value from the local environment instead of committing it:

```yaml
provider_key_sources:
  fireworks: user
provider_key_envs:
  fireworks: FIREWORKS_API_KEY
```

```bash
export FIREWORKS_API_KEY=fw_...
dari router create ./router.yml
```

Never commit provider keys or `.env` files.

## Custom Rules

Use `routing_strategy: custom` and `custom_config` for natural-language routing rules:

```yaml
routing_strategy: custom
custom_config:
  rules:
    - when: planning and architecture
      use: openai/gpt-5.6-sol
      thinking_level: high
  default: deepseek-ai/DeepSeek-V4-Flash-0731
  default_thinking_level: off
```

When `model_thinking_levels` is present, it must list every enabled model:

```yaml
model_thinking_levels:
  openai/gpt-5.6-sol: [low, medium, high]
  deepseek-ai/DeepSeek-V4-Flash-0731: [off]
```

## Evals

Create an organization scorecard from a CSV whose first two columns are `model_id,score`. Optional columns are `thinking_level`, `notes`, and `metadata_json`:

```bash
dari eval create --name "SWE-bench Verified" --file scores.csv
```

Each `(model_id, thinking_level)` pair must be unique. Use `--file -` to read the CSV from standard input.

`model_id` values must be the exact provider-prefixed IDs from the router model catalog — run `dari router models` first and copy IDs from its output. Do not invent aliases, bare provider names, or IDs the catalog does not list:

```csv
model_id,score,thinking_level,notes
openai/gpt-5.6-sol,87,high,Strong public run
anthropic/claude-fable-5,81,,Generic score
```

Then list scorecards and use their exact IDs:

```bash
dari eval list
```

```yaml
eval_ids:
  - evl_...
```

Do not commit organization-specific eval IDs to public examples.

## Call A Router

Create a routing API key and use the endpoint returned by `dari router get`:

```bash
dari api-keys create --name app --type routing
export DARI_ROUTING_API_KEY=dari_...

curl https://routing.dari.dev/rtr_.../chat/completions \
  -H "Authorization: Bearer $DARI_ROUTING_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dari/routing",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

The response is OpenAI-compatible and includes `dari_routing` metadata with the selected model and reasoning effort.

## Launch A Coding Agent

Use an installed local coding agent through Dari without editing the agent's configuration:

```bash
dari --claude
dari --codex
dari --pi
```

The first Claude or Codex launch opens an arrow-key model checklist (up/down to move, space to toggle, right arrow to check any combination of thinking levels). Codex preserves the current default router selection unless it is changed; Claude gets a separate router with the recommended coding models preselected and the current default public eval set. Dari applies recommended reasoning levels and managed providers to selected models, then privately caches the agent router and Routing key. Set `DARI_ROUTING_API_KEY` to supply the key explicitly. Every following argument is forwarded to the selected agent, and Dari's provider/model settings take precedence:

```bash
dari --claude --print "Review this diff"
dari --codex exec "Fix the failing tests"
dari --pi -p "Review this repository"
```

## Inspect, Update, Delete

```bash
dari router list
dari router get <router-id-or-endpoint>
dari router update <router-id-or-endpoint> --model openai/gpt-5.6-sol
dari router delete <router-id-or-endpoint>
```

## Examples

- Managed-router manifests: https://github.com/mupt-ai/dari-router/tree/main/examples/managed
- Router Framework documentation: https://docs.dari.dev/framework/overview
- Router Framework examples: https://github.com/mupt-ai/dari-router/tree/main/examples
- Managed router docs: https://docs.dari.dev/router/create-a-router
