from __future__ import annotations

from claude_agent_sdk import query


async def agent(prompt: str = "Introduce yourself.") -> str:
    text: list[str] = []

    async for message in query(prompt=prompt):
        result = getattr(message, "result", None)
        if isinstance(result, str):
            text.append(result)

    return "\n".join(text).strip()
