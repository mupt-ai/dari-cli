# Hello Claude Agent SDK Python Example

This example exists to exercise `dari manifest validate`, `dari deploy`, and a
local import smoke test for the Claude Agent SDK Python package.

Run:

```bash
uv run dari manifest validate examples/hello-claude-agent-sdk-python --json
uv run dari deploy examples/hello-claude-agent-sdk-python --dry-run
cd examples/hello-claude-agent-sdk-python && uv run --with-requirements requirements.txt python smoke_test.py
```
