# Hello OpenAI Agents Python Example

This example exists to exercise `dari manifest validate`, `dari deploy`, and a
local import smoke test for the OpenAI Agents SDK Python package.

Run:

```bash
uv run dari manifest validate examples/hello-openai-agents-python --json
uv run dari deploy examples/hello-openai-agents-python --dry-run
cd examples/hello-openai-agents-python && uv run --with-requirements requirements.txt python smoke_test.py
```
