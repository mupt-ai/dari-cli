# Dari CLI

`dari` is the CLI for packaging an agent checkout, validating `dari.yml`, and
publishing agent versions to Agent Host.

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

Validate a manifest:

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

Create or inspect execution backends for Pi deploys:

```bash
dari execution-backends create --name "Primary E2B"
dari execution-backends list
```

Deploy the current checkout:

```bash
dari deploy .
```

For `sdk: pi`, you must also provide the execution backend to pin for that
publish:

```bash
dari deploy . --execution-backend-id execb_123
```

Or set it through the environment:

```bash
DARI_EXECUTION_BACKEND_ID=execb_123 dari deploy .
```

This management flow uses the browser login session from `dari auth login` and
the currently selected org.

The CLI talks to `https://api.dari.dev`.

## Pi Deploys

Pi deploys require an execution backend ID pinned at publish time.

Create an E2B-backed execution backend for the current org:

```bash
dari execution-backends create --name "Primary E2B"
```

The command prompts for the E2B API key securely. Use
`--e2b-api-key-stdin` when you want to provide it from another command.

List existing execution backends and copy the returned `execb_*` ID:

```bash
dari execution-backends list
```

Deploy the Pi repo with that backend pinned:

```bash
dari deploy . --execution-backend-id execb_123
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
