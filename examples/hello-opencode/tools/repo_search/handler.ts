export async function main(input: { query: string }) {
  return {
    matches: [`hello-opencode matched: ${input.query}`],
  };
}
