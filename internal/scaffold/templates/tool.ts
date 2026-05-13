import { readdir, readFile, stat } from "node:fs/promises";
import { join, relative } from "node:path";

const DEFAULT_MAX_RESULTS = 20;
const MAX_MAX_RESULTS = 100;
const MAX_FILE_BYTES = 1_000_000;
const IGNORED_DIRECTORIES = new Set([
  ".dari",
  ".git",
  ".next",
  ".turbo",
  "build",
  "coverage",
  "dist",
  "node_modules",
]);

type SearchInput = {
  query: string;
  max_results?: number;
};

type SearchMatch = {
  path: string;
  line: number;
  preview: string;
};

export const description = "Case-insensitive content search over the checked-out repository.";

export const inputSchema = {
  type: "object",
  properties: {
    query: {
      type: "string",
      minLength: 1,
    },
    max_results: {
      type: "integer",
      minimum: 1,
      maximum: 100,
      default: 20,
    },
  },
  required: ["query"],
  additionalProperties: false,
};

export async function handler(input: SearchInput) {
  if (typeof input?.query !== "string") {
    throw new Error("repo_search query must be a non-empty string.");
  }
  const query = input.query.trim();
  if (!query) {
    throw new Error("repo_search query must be a non-empty string.");
  }

  const root = process.env.DARI_SOURCE_BUNDLE_ROOT || process.cwd();
  const maxResults = normalizeMaxResults(input.max_results);
  const matches: SearchMatch[] = [];
  let scanned_files = 0;
  let skipped_binary_files = 0;
  let skipped_large_files = 0;
  let total_matches = 0;

  for await (const filePath of walkFiles(root)) {
    const fileStat = await stat(filePath);
    if (fileStat.size > MAX_FILE_BYTES) {
      skipped_large_files += 1;
      continue;
    }

    const buffer = await readFile(filePath);
    if (buffer.includes(0)) {
      skipped_binary_files += 1;
      continue;
    }

    scanned_files += 1;
    const text = buffer.toString("utf-8");
    const relativePath = relative(root, filePath) || ".";
    const lineMatches = searchLines(text, query, relativePath);
    total_matches += lineMatches.length;
    for (const match of lineMatches) {
      if (matches.length < maxResults) {
        matches.push(match);
      }
    }
  }

  return {
    query,
    matches,
    total_matches,
    truncated: total_matches > matches.length,
    scanned_files,
    skipped_binary_files,
    skipped_large_files,
  };
}

function normalizeMaxResults(value: number | undefined): number {
  if (value === undefined) {
    return DEFAULT_MAX_RESULTS;
  }
  if (!Number.isInteger(value) || value < 1) {
    throw new Error("repo_search max_results must be a positive integer.");
  }
  return Math.min(value, MAX_MAX_RESULTS);
}

async function* walkFiles(root: string): AsyncGenerator<string> {
  const entries = await readdir(root, { withFileTypes: true });
  entries.sort((left, right) => left.name.localeCompare(right.name));

  for (const entry of entries) {
    const entryPath = join(root, entry.name);
    if (entry.isDirectory()) {
      if (!IGNORED_DIRECTORIES.has(entry.name)) {
        yield* walkFiles(entryPath);
      }
      continue;
    }
    if (entry.isFile()) {
      yield entryPath;
    }
  }
}

function searchLines(text: string, query: string, path: string): SearchMatch[] {
  const needle = query.toLowerCase();
  return text
    .split(/\r?\n/)
    .map((line, index) => ({ line, index }))
    .filter(({ line }) => line.toLowerCase().includes(needle))
    .map(({ line, index }) => ({
      path,
      line: index + 1,
      preview: line.trim().slice(0, 240),
    }));
}
