# Hello OpenAI Agents JavaScript Example

This example exists to exercise `dari manifest validate`, `dari deploy`, and a
local import smoke test for the OpenAI Agents SDK JavaScript package.

Run:

```bash
uv run dari manifest validate examples/hello-openai-agents-js --json
uv run dari deploy examples/hello-openai-agents-js --dry-run
cd examples/hello-openai-agents-js && npm install --no-package-lock && npm run smoke-test
```
