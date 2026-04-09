from agent import agent


if getattr(agent, "name", None) != "hello-openai-agents-python":
    raise SystemExit("Expected hello-openai-agents-python agent export.")

print("hello-openai-agents-python smoke test passed")
