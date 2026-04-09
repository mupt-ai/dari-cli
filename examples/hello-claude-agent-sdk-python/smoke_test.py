import inspect

from agent import agent


if not inspect.iscoroutinefunction(agent):
    raise SystemExit("Expected hello-claude-agent-sdk-python.agent to be async.")

print("hello-claude-agent-sdk-python smoke test passed")
