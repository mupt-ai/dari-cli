import { pathToFileURL } from "node:url";

const [, , modulePath] = process.argv;
if (!modulePath) {
  throw new Error("usage: extractor <module-path>");
}

const mod = await import(pathToFileURL(modulePath).href);

if (typeof mod.handler !== "function") {
  throw new Error(`${modulePath} must export a handler function.`);
}

function stringExport(name) {
  const value = mod[name];
  if (typeof value !== "string" || value.trim() === "") {
    throw new Error(`${modulePath} must export a non-empty string named '${name}'.`);
  }
  return value.trim();
}

function schemaExport(name, required) {
  const value = mod[name];
  if (value == null) {
    if (required) throw new Error(`${modulePath} must export a JSON Schema object named '${name}'.`);
    return undefined;
  }
  if (typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${modulePath} export '${name}' must be a JSON Schema object.`);
  }
  JSON.stringify(value);
  return value;
}

const payload = {
  description: stringExport("description"),
  input_schema: schemaExport("inputSchema", true),
};
const outputSchema = schemaExport("outputSchema", false);
if (outputSchema != null) payload.output_schema = outputSchema;

process.stdout.write(JSON.stringify(payload));
