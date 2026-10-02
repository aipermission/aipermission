import fs from "node:fs/promises";
import { parse as parseTOML } from "smol-toml";
import { atomicWritePrivateFile, withPrivateFileLock } from "./private-file.js";

export async function writeTOMLMCPConfig(filePath, name, config, options = {}) {
  await withPrivateFileLock(
    filePath,
    async () => {
      await options.beforeWrite?.();
      let current = "";
      try {
        current = await fs.readFile(filePath, "utf8");
      } catch (error) {
        if (error.code !== "ENOENT") throw error;
      }
      const next = removeTOMLServer(current, name).trimEnd();
      const block = tomlServerBlock(name, config);
      const outputContent = `${next ? `${next}\n\n` : ""}${block}\n`;
      parseTOMLDocument(outputContent, filePath);
      await options.beforeWrite?.();
      await atomicWritePrivateFile(filePath, outputContent, options);
    },
    options,
  );
}

function removeTOMLServer(source, name) {
  if (!source.trim()) return source;
  parseTOMLDocument(source, "existing TOML config");
  const lines = source.split(/\r?\n/);
  const kept = [];
  let skipping = false;
  const scanState = { multiline: "" };
  for (const line of lines) {
    const header = scanTOMLHeader(line, scanState);
    const selected = header?.[0] === "mcp_servers" && header?.[1] === name;
    if (selected) {
      skipping = true;
      continue;
    }
    if (header && skipping) {
      skipping = false;
    }
    if (!skipping) {
      kept.push(line);
    }
  }
  return kept.join("\n");
}

function scanTOMLHeader(line, state) {
  const trimmed = line.trimStart();
  if (!state.multiline && trimmed.startsWith("[")) {
    try {
      return findMarkerPath(parseTOML(`${line}\n__aipermission_header_marker = true\n`));
    } catch {
      // A string can start with a bracket; scan it before the next line.
    }
  }
  scanTOMLStrings(line, state);
  return null;
}

function scanTOMLStrings(line, state) {
  let quote = state.multiline;
  for (let index = 0; index < line.length; index += 1) {
    const character = line[index];
    if (!quote && character === "#") break;
    if (quote.startsWith('"') && character === "\\") {
      index += 1;
      continue;
    }
    if (quote) {
      if (line.startsWith(quote, index)) {
        index += quote.length - 1;
        quote = "";
      }
      continue;
    }
    if (character !== '"' && character !== "'") continue;
    const triple = character.repeat(3);
    quote = line.startsWith(triple, index) ? triple : character;
    index += quote.length - 1;
  }
  state.multiline = quote.length === 3 ? quote : "";
}

function findMarkerPath(value, pathParts = []) {
  if (!value || typeof value !== "object") return null;
  if (value.__aipermission_header_marker === true) return pathParts;
  for (const [key, nested] of Object.entries(value)) {
    const match = findMarkerPath(nested, [...pathParts, key]);
    if (match) return match;
  }
  return null;
}

function parseTOMLDocument(contents, location) {
  try {
    return parseTOML(contents);
  } catch (error) {
    error.message = "TOML parsing failed";
    error.stack = `${error.name || "Error"}: ${error.message}`;
    throw new Error(`Could not parse TOML config at ${location}; the existing file was left unchanged`, { cause: error });
  }
}

function tomlServerBlock(name, config) {
  const fields = Object.entries(config)
    .filter(([key]) => key !== "env")
    .map(([key, value]) => `${key} = ${tomlValue(value)}`);
  return `[mcp_servers.${tomlKey(name)}]
${fields.join("\n")}
enabled = true

[mcp_servers.${tomlKey(name)}.env]
NODE_ENV = ${tomlString(config.env.NODE_ENV)}
AIPERMISSION_API_URL = ${tomlString(config.env.AIPERMISSION_API_URL)}
AIPERMISSION_API_TOKEN = ${tomlString(config.env.AIPERMISSION_API_TOKEN)}`;
}

// Keep stdout rendering separate from the secret-bearing TOML writer.
export function tomlPreviewServerBlock(name, config) {
  const fields = Object.entries(config)
    .filter(([key]) => key !== "env")
    .map(([key, value]) => `${key} = ${tomlValue(value)}`);
  return `[mcp_servers.${tomlKey(name)}]
${fields.join("\n")}
enabled = true

[mcp_servers.${tomlKey(name)}.env]
NODE_ENV = "production"
AIPERMISSION_API_URL = ${tomlString(config.env.AIPERMISSION_API_URL)}
AIPERMISSION_API_TOKEN = "YOUR_TOKEN_HERE"`;
}

export function tomlKey(value) {
  if (/^[A-Za-z0-9_-]+$/.test(value)) {
    return value;
  }
  return tomlString(value);
}

export function tomlString(value) {
  return JSON.stringify(String(value));
}

function tomlValue(value) {
  if (typeof value === "string") return tomlString(value);
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  if (Array.isArray(value)) return `[${value.map(tomlValue).join(", ")}]`;
  throw new Error(`Unsupported TOML MCP config value: ${typeof value}`);
}
