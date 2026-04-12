# Hello Pi Example

This example exists to exercise `dari manifest validate`,
`dari deploy --dry-run`, and a local smoke test against a managed bundle
targeting `pi`.

Pi deploys require an execution backend ID.
Create one with
`uv run dari execution-backends create --name "Primary E2B" --provider e2b`
and pass the returned `execb_*` value to deploys.

Run:

```bash
uv run dari manifest validate examples/hello-pi --json
uv run dari deploy examples/hello-pi --dry-run --execution-backend-id execb_123
cd examples/hello-pi && npm install --no-package-lock && npm run smoke-test
```
