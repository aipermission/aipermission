const commonPathFlags = {
  home: "value",
  "project-dir": "value",
};

const schemas = {
  init: {
    provider: "value",
    scope: "value",
    name: "value",
    "api-url": "value",
    "token-stdin": "boolean",
    print: "boolean",
    force: "boolean",
    ...commonPathFlags,
  },
  setup: {
    provider: "value",
    scope: "value",
    "skill-scope": "value",
    name: "value",
    "api-url": "value",
    "token-stdin": "boolean",
    print: "boolean",
    force: "boolean",
    "skill-source": "value",
    ...commonPathFlags,
  },
  "install-skill": {
    client: "value",
    scope: "value",
    source: "value",
    ...commonPathFlags,
  },
  doctor: {
    client: "value",
    provider: "value",
    scope: "value",
    "mcp-scope": "value",
    "skill-scope": "value",
    name: "value",
    ...commonPathFlags,
  },
};

export function parseCommandFlags(command, argv = []) {
  const schema = schemas[command];
  if (!schema) throw new Error(`Unknown command schema: ${command}`);
  const result = {};
  for (let index = 0; index < argv.length; index += 1) {
    const argument = argv[index];
    if (!argument.startsWith("--")) throw new Error(`${command} does not accept positional argument: ${argument}`);
    const separator = argument.indexOf("=");
    const rawKey = argument.slice(2, separator < 0 ? undefined : separator);
    if (!rawKey) throw new Error(`${command} received an empty option name`);
    if (rawKey === "token") throw new Error("--token is not supported; use the hidden prompt or --token-stdin");
    const kind = schema[rawKey];
    if (!kind) throw new Error(`Unknown ${command} option: --${rawKey}`);
    const key = camelCase(rawKey);
    if (Object.hasOwn(result, key)) throw new Error(`Duplicate ${command} option: --${rawKey}`);
    const inlineValue = separator < 0 ? undefined : argument.slice(separator + 1);
    if (kind === "boolean") {
      result[key] = parseBooleanOption(rawKey, inlineValue);
      continue;
    }
    const value = inlineValue === undefined ? argv[++index] : inlineValue;
    if (!value || value.startsWith("--")) throw new Error(`--${rawKey} requires a non-empty value`);
    result[key] = value;
  }
  return result;
}

function parseBooleanOption(key, value) {
  if (value === undefined || value === "true") return true;
  if (value === "false") return false;
  throw new Error(`--${key} accepts only true or false`);
}

function camelCase(value) {
  return value.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
}
