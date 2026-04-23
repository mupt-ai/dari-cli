export async function main(input: { query: string }) {
  return {
    matches: [`matched: ${input.query}`],
  };
}
