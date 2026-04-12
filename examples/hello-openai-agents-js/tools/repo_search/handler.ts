export async function main(input: { query: string }) {
  return {
    matches: [`hello-openai-agents-js matched: ${input.query}`],
  };
}
