# Dari CLI

`dari` is the CLI for packaging an agent checkout, validating `dari.yml`, and
publishing agent versions to Agent Host.

This repository is a public mirror of the CLI subtree from
`mupt-ai/agent-host`. The source of truth lives in the monorepo at
`services/cli`, and this mirror is updated from there.

## Install

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

By default, the CLI talks to `https://api.dari.dev`. Override that with
`--api-url` or `DARI_API_URL`.

## Local Development

Install dependencies and run tests:

```bash
uv sync --group dev
uv run pytest
```

Examples live under [examples](./examples).

## Contributing

Read [CONTRIBUTING.md](./CONTRIBUTING.md) before opening a PR. This public repo
accepts issues and PRs for review, but accepted changes are replayed into the
monorepo first and then mirrored back here.
