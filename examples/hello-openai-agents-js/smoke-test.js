import { agent } from "./src/agent.js";

if (!agent || typeof agent !== "object") {
  throw new Error(
    "Expected hello-openai-agents-js to export an OpenAI agent object.",
  );
}

console.log("hello-openai-agents-js smoke test passed");
