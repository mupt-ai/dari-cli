export async function main(input: { query: string }) {
  return {
    matches: [`hello-pi matched: ${input.query}`],
  };
}
