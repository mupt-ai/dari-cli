import { agent } from "./src/agent.js";

if (typeof agent !== "function") {
  throw new Error(
    "Expected hello-claude-agent-sdk-js to export a function named agent.",
  );
}

console.log("hello-claude-agent-sdk-js smoke test passed");
