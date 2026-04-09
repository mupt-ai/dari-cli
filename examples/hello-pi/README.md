# Hello Pi Example

This example exists to exercise `dari manifest validate`, `dari deploy`, and a
local import smoke test for the Pi SDK example.

Run:

```bash
uv run dari manifest validate examples/hello-pi --json
uv run dari deploy examples/hello-pi --dry-run
cd examples/hello-pi && npm install --no-package-lock && npm run smoke-test
```
