import { agent } from "./src/agent.js";

if (typeof agent !== "function") {
  throw new Error("Expected hello-pi to export an async function named agent.");
}

console.log("hello-pi smoke test passed");
