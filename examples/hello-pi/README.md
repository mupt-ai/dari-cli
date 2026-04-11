# Hello Pi Example

This example exists to exercise `dari manifest validate` and
`dari deploy --dry-run` against a managed bundle targeting `pi`.

Run:

```bash
uv run dari manifest validate examples/hello-pi --json
uv run dari deploy examples/hello-pi --dry-run
```
