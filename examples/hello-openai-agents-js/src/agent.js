import { Agent } from "@openai/agents";

export const agent = new Agent({
  name: "hello-openai-agents-js",
  instructions:
    "You are a minimal OpenAI Agents SDK example used to verify Dari manifest validation and deploy packaging.",
});
