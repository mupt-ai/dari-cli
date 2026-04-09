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

Deploy the current checkout:

```bash
dari deploy .
```

The CLI talks to `https://api.dari.dev`.

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
