# Hello Claude Agent SDK Python Example

This example exists to exercise `dari manifest validate` and
`dari deploy --dry-run` against a managed bundle targeting `claude-agent-sdk`.

Run:

```bash
uv run dari manifest validate examples/hello-claude-agent-sdk-python --json
uv run dari deploy examples/hello-claude-agent-sdk-python --dry-run
```
