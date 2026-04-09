import { query } from "@anthropic-ai/claude-agent-sdk";

export async function agent(prompt = "Introduce yourself.") {
  const text = [];

  for await (const message of query({ prompt })) {
    if ("result" in message && typeof message.result === "string") {
      text.push(message.result);
    }
  }

  return text.join("\n").trim();
}
