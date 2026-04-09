# Contributing

This repository is a public mirror of the CLI that lives in the private
`mupt-ai/agent-host` monorepo under `services/cli`.

## Source Of Truth

The monorepo is canonical.

Do not treat `main` in this mirror as the place where maintainers make direct
source-of-truth edits. Mirror sync may rewrite branch history so that this repo
matches the monorepo subtree.

## How Pull Requests Work

You can open a PR in this public repo for discussion and review.

If maintainers accept the change, they will replay it into the monorepo first.
After that lands upstream, the mirror automation pushes the updated subtree back
to this repo.

That means:

- PRs here may be closed in favor of an upstream change.
- Commit SHAs in this repo are not stable integration points.
- Maintainers should not merge ad hoc commits directly into this mirror unless
  they also carry the same change upstream immediately.

## Local Development

```bash
uv sync --group dev
uv run pytest
```

## License

No standalone license file is included here yet. Until one is added, public
visibility does not grant open-source license rights.
