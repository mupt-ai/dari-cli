# Hello Claude Agent SDK JavaScript Example

This example exists to exercise `dari manifest validate`, `dari deploy`, and a
local import smoke test for the Claude Agent SDK JavaScript package.

Run:

```bash
uv run dari manifest validate examples/hello-claude-agent-sdk-js --json
uv run dari deploy examples/hello-claude-agent-sdk-js --dry-run
cd examples/hello-claude-agent-sdk-js && npm install --no-package-lock && npm run smoke-test
```
