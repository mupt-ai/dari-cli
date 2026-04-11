# Dari CLI

`dari` is the CLI for validating a managed deployment bundle, packaging the
current checkout, and publishing agent versions to Agent Host.

Full platform docs live at `https://docs.dari.dev`.

## Install

After the package is published to PyPI:

```bash
uv tool install dari-cli
```

Or:

```bash
pipx install dari-cli
```

Install directly from GitHub before a PyPI release:

```bash
uv tool install git+https://github.com/mupt-ai/dari-cli.git
```

Or:

```bash
pipx install git+https://github.com/mupt-ai/dari-cli.git
```

## Commands

Validate a managed bundle:

```bash
dari manifest validate .
```

Log in through the browser:

```bash
dari auth login
```

Manage runtime credentials for the current org:

```bash
dari credentials add OPENAI_API_KEY
dari credentials list
```

Deploy the current checkout:

```bash
dari deploy .
```

The CLI talks to `https://api.dari.dev`.

## Managed Bundle Shape

`dari` expects a repo-root bundle with:

- `dari.yml`
- `Dockerfile`
- any prompt files referenced by `instructions`
- discovered custom tools under `tools/<name>/tool.yml`

Minimal example:

```yaml
name: support-agent
harness: opencode

instructions:
  system: prompts/system.md

runtime:
  dockerfile: Dockerfile

tools:
  - name: repo_search
    path: tools/repo_search
    kind: main
  - name: sandbox.exec
    kind: ephemeral

env:
  APP_ENV: production
```

Example custom tool definition:

```yaml
name: repo_search
description: Search the checked-out repository for matching content.
input_schema: input.schema.json
runtime: typescript
handler: handler.ts:main
retries: 2
timeout_seconds: 20
```

## Local Development

Install dependencies and run tests:

```bash
uv sync --group dev
uv run pytest
```

Examples live under [examples](./examples).

## Release

1. Update `version` in `pyproject.toml`.
2. Run `uv build` and `uv run pytest`.
3. Configure PyPI trusted publishing for the `Publish CLI` GitHub Actions workflow.
4. Push a tag like `v0.1.1`.
5. GitHub Actions publishes the tagged build to PyPI.

## Contributing

Read [CONTRIBUTING.md](./CONTRIBUTING.md) before opening a PR.
