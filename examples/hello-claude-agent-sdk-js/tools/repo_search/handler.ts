export async function main(input: { query: string }) {
  return {
    matches: [`hello-claude-agent-sdk-js matched: ${input.query}`],
  };
}
