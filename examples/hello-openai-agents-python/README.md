# Hello OpenAI Agents Python Example

This example exists to exercise `dari manifest validate` and
`dari deploy --dry-run` against a managed bundle targeting `openai-agents`.

Run:

```bash
uv run dari manifest validate examples/hello-openai-agents-python --json
uv run dari deploy examples/hello-openai-agents-python --dry-run
```
