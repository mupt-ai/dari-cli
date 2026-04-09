import { createAgentSession } from "@mariozechner/pi-coding-agent";

export async function agent(
  prompt = "List the files in the current directory.",
) {
  const { session } = await createAgentSession();
  const text = [];

  session.subscribe((event) => {
    if (
      event.type === "message_update" &&
      event.assistantMessageEvent.type === "text_delta"
    ) {
      text.push(event.assistantMessageEvent.delta);
    }
  });

  await session.prompt(prompt);
  return text.join("");
}
