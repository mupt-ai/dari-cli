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
  default: fireworks/deepseek-ai/DeepSeek-V4-Flash-0731
  default_thinking_level: off
```

When `model_thinking_levels` is present, it must list every enabled model:

```yaml
model_thinking_levels:
  openai/gpt-5.6-sol: [low, medium, high]
  fireworks/deepseek-ai/DeepSeek-V4-Flash-0731: [off]
```

## Evals

List available scorecards and use their exact IDs:

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

## Inspect, Update, Delete

```bash
dari router list
dari router get <router-id-or-endpoint>
dari router update <router-id-or-endpoint> --model openai/gpt-5.6-sol
dari router delete <router-id-or-endpoint>
```

## Examples

- Managed-router manifests: https://github.com/mupt-ai/dari-dev-examples
- Router Framework documentation: https://docs.dari.dev/framework/overview
- Managed router docs: https://docs.dari.dev/router/create-a-router
