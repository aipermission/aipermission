import fs from "node:fs/promises";
import { execFile } from "node:child_process";
import { createRequire } from "node:module";
import path from "node:path";
import readline from "node:readline/promises";
import { promisify } from "node:util";
import { pathToFileURL } from "node:url";
import { stdin as input, stdout as output } from "node:process";
import { parse as parseTOML } from "smol-toml";
import { parseCommandFlags } from "./cli-flags.js";
import { DEFAULT_API_URL, normalizeLocalAPIURL } from "./local-url.js";
import { adaptMCPServerConfig, getClient, MCP_PROVIDERS, resolveMCPConfigTarget, resolveMCPPrintTarget } from "./client-registry.js";
import { commitSkillInstallation, prepareSkillInstallation } from "./install-skill.js";
import {
  atomicWritePrivateFile,
  privateLockPath,
  privateStagingIgnorePath,
  privateStagingPath,
  privateTemporaryIgnorePath,
  privateTemporaryPath,
  withPrivateFileLock,
} from "./private-file.js";

const require = createRequire(import.meta.url);
const execFileAsync = promisify(execFile);
const packageMetadata = require("../package.json");

export const PACKAGE_NAME = packageMetadata.name;
export const PACKAGE_VERSION = packageMetadata.version;
export const PACKAGE_SPECIFIER = `${PACKAGE_NAME}@${PACKAGE_VERSION}`;
const useColor = output.isTTY && !process.env.NO_COLOR;
const color = {
  reset: useColor ? "\x1b[0m" : "",
  bold: useColor ? "\x1b[1m" : "",
  dim: useColor ? "\x1b[2m" : "",
  green: useColor ? "\x1b[32m" : "",
  cyan: useColor ? "\x1b[36m" : "",
  yellow: useColor ? "\x1b[33m" : "",
};

export async function runInit(argv = []) {
  return runConfiguration("init", argv);
}

export async function runSetup(argv = []) {
  return runConfiguration("setup", argv);
}

async function runConfiguration(command, argv) {
  const flags = parseCommandFlags(command, argv);
  const interactive = Boolean(input.isTTY && output.isTTY);
  assertProviderSelectionAvailable(flags.provider, interactive);
  const rl = readline.createInterface({ input, output });
  try {
    const provider = flags.provider
      ? findProvider(flags.provider)
      : await selectProvider("Which AI client should use this token?", MCP_PROVIDERS);
    const name = sanitizeName(flags.name || (interactive ? await ask(rl, "MCP server name", "aipermission") : "aipermission"));
    const apiUrl = normalizeURL(flags.apiUrl || DEFAULT_API_URL);
    const outputTarget =
      provider.id === "custom"
        ? undefined
        : flags.print
          ? resolveMCPPrintTarget(provider.id, flags.scope)
          : resolveMCPConfigTarget(provider.id, flags.scope);
    const shouldPrepareSkill = command === "setup" && (!flags.print || provider.id === "custom");
    const preparedSkill = shouldPrepareSkill ? await prepareSetupSkill(provider.id, flags) : undefined;
    if (provider.id === "custom" || flags.print) {
      const skillResult = preparedSkill ? await reportInstalledSkill(preparedSkill) : undefined;
      printPlaceholderConfigNotice();
      printProviderConfig(name, provider.id, apiUrl, outputTarget);
      if (command === "setup" && flags.print && provider.id !== "custom") {
        console.error("No files were changed. Run install-skill separately to install the native operator skill.");
      }
      return { provider: provider.id, name, printed: true, skill: skillResult };
    }

    const stdinToken = flags.tokenStdin ? (await readStdin()).trim() : "";
    const token = await resolveToken({ ...flags, stdinToken }, rl);
    if (!token) {
      throw new Error("API token is required.");
    }
    const config = adaptMCPServerConfig(provider.id, buildMCPServerConfig({ apiUrl, token }));
    const skillResult = preparedSkill ? await reportInstalledSkill(preparedSkill) : undefined;
    const result = await writeProviderConfig(provider.id, name, config, {
      force: Boolean(flags.force),
      scope: flags.scope,
      homeDir: flags.home,
      projectDir: flags.projectDir,
    });
    console.log("");
    console.log(`${color.green}Configured ${provider.label}${color.reset}`);
    console.log(`${color.dim}Name:${color.reset} ${name}`);
    console.log(`${color.dim}Path:${color.reset} ${result.path}`);
    console.log(`${color.dim}Scope:${color.reset} ${result.scope}`);
    if (result.gitExcluded) {
      console.log(`${color.dim}Git:${color.reset} added ${result.gitExcludeEntry} to .git/info/exclude`);
    }
    console.log("");
    console.log(
      `${color.yellow}Keep this config private:${color.reset} it contains an AIPermission bearer token. If it is committed, revoke the token.`,
    );
    console.log(`${color.yellow}Restart the AI client so it reloads MCP servers.${color.reset}`);
    return { provider: provider.id, name, config: result, skill: skillResult };
  } finally {
    rl.close();
  }
}

export function assertProviderSelectionAvailable(provider, interactive) {
  if (!provider && !interactive) {
    throw new Error("Non-interactive setup requires an explicit --provider.");
  }
}

export function parseFlags(argv) {
  return parseCommandFlags("init", argv);
}

async function prepareSetupSkill(client, flags) {
  return prepareSkillInstallation({
    client,
    scope: flags.skillScope || flags.scope,
    source: flags.skillSource,
    homeDir: flags.home,
    projectDir: flags.projectDir,
  });
}

async function reportInstalledSkill(prepared) {
  const result = await commitSkillInstallation(prepared);
  if (!result.path) {
    console.log("");
    console.log(result.content);
    return result;
  }
  console.log("");
  console.log(`${color.green}Installed native operator skill${color.reset}`);
  console.log(`${color.dim}Path:${color.reset} ${result.path}`);
  console.log(`${color.dim}Scope:${color.reset} ${result.scope}`);
  return result;
}

function findProvider(idOrLabel) {
  try {
    return getClient(idOrLabel);
  } catch {
    throw new Error(`Unknown provider: ${idOrLabel}`);
  }
}

async function selectProvider(title, items) {
  if (!input.isTTY || !output.isTTY) {
    return items[0];
  }

  let index = 0;
  let renderedLines = 0;
  input.setRawMode(true);
  input.resume();

  const render = () => {
    if (renderedLines > 0) {
      output.write(`\x1b[${renderedLines}A`);
      output.write("\x1b[J");
    }
    const lines = [`${color.bold}${color.cyan}${title}${color.reset}`, `${color.dim}Use ↑/↓ and Enter.${color.reset}`, ""];
    for (let i = 0; i < items.length; i += 1) {
      const selected = i === index;
      const marker = selected ? `${color.green}›${color.reset}` : " ";
      const label = selected ? `${color.bold}${items[i].label}${color.reset}` : items[i].label;
      lines.push(`${marker} ${label} ${color.dim}- ${items[i].description}${color.reset}`);
    }
    output.write("\x1b[?25l");
    output.write(`${lines.join("\n")}\n`);
    renderedLines = lines.length;
  };

  render();

  return await new Promise((resolve) => {
    const cleanup = (selected) => {
      input.off("data", onData);
      input.setRawMode(false);
      output.write("\x1b[?25h");
      if (renderedLines > 0) {
        output.write(`\x1b[${renderedLines}A`);
        output.write("\x1b[J");
      }
      if (selected) {
        output.write(`${color.green}Selected:${color.reset} ${selected.label}\n`);
      }
    };
    const onData = (buffer) => {
      const value = buffer.toString("utf8");
      const keys = splitInputKeys(value);
      for (const key of keys) {
        if (key === "\u0003") {
          cleanup();
          process.exit(130);
        }
        if (key === "\r" || key === "\n") {
          const selected = items[index];
          cleanup(selected);
          resolve(selected);
          return;
        }
        if (key === "\u001b[A") {
          index = (index - 1 + items.length) % items.length;
          render();
          continue;
        }
        if (key === "\u001b[B") {
          index = (index + 1) % items.length;
          render();
        }
      }
    };
    input.on("data", onData);
  });
}

async function ask(rl, label, defaultValue = "") {
  const suffix = defaultValue ? ` (${defaultValue})` : "";
  const answer = await rl.question(`${label}${suffix}: `);
  return answer.trim() || defaultValue;
}

async function resolveToken(flags, rl) {
  if (flags.tokenStdin) {
    return flags.stdinToken;
  }
  return askSecret(rl, "API token");
}

async function askSecret(rl, label) {
  if (!input.isTTY || !output.isTTY) {
    const answer = await rl.question(`${label}: `);
    return answer.trim();
  }

  rl.pause();
  output.write(`${label}: `);
  input.setRawMode(true);
  input.resume();

  let value = "";
  return await new Promise((resolve) => {
    const cleanup = () => {
      input.off("data", onData);
      input.setRawMode(false);
      output.write("\n");
      rl.resume();
    };
    const onData = (buffer) => {
      const text = buffer.toString("utf8");
      for (const char of text) {
        if (char === "\u0003") {
          cleanup();
          process.exit(130);
        }
        if (char === "\r" || char === "\n") {
          cleanup();
          resolve(value.trim());
          return;
        }
        if (char === "\u007f" || char === "\b") {
          value = value.slice(0, -1);
          continue;
        }
        value += char;
      }
    };
    input.on("data", onData);
  });
}

async function readStdin() {
  const chunks = [];
  for await (const chunk of input) {
    chunks.push(Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

export function buildMCPServerConfig({ apiUrl, token }) {
  return {
    command: "npx",
    args: ["-y", PACKAGE_SPECIFIER],
    env: {
      NODE_ENV: "production",
      AIPERMISSION_API_URL: apiUrl,
      AIPERMISSION_API_TOKEN: token,
    },
  };
}

export async function writeProviderConfig(providerID, name, config, options = {}) {
  const projectRoot = options.projectDir || process.cwd();
  const target = resolveMCPConfigTarget(providerID, options.scope, {
    homeDir: options.homeDir,
    projectDir: projectRoot,
    env: options.env,
  });
  const providerConfig = adaptMCPServerConfig(providerID, config);
  let protection = {};
  if (target.projectConfig) {
    await assertProjectConfigWritable(target.path, options);
    protection = await protectGitIgnoredConfig(target.path, projectRoot, { allowTracked: Boolean(options.force) });
  }
  const trustedRoot = target.trustedRoot;
  const writeOptions = {
    trustedRoot,
    beforeWrite: target.projectConfig
      ? async () => {
          await assertProjectConfigWritable(target.path, options);
          await options.beforeWrite?.();
          await assertProjectConfigWritable(target.path, options);
        }
      : options.beforeWrite,
  };
  if (target.format === "toml") {
    await writeTOMLMCPConfig(target.path, name, providerConfig, writeOptions);
  } else {
    await writeJSONMCPConfig(target.path, name, providerConfig, target.rootKey, writeOptions);
  }
  return { path: target.path, scope: target.scope, ...protection };
}

export async function writeJSONMCPConfig(filePath, name, config, rootKey, options = {}) {
  await withPrivateFileLock(
    filePath,
    async () => {
      await options.beforeWrite?.();
      let root = {};
      try {
        root = JSON.parse(await fs.readFile(filePath, "utf8"));
      } catch (error) {
        if (error.code !== "ENOENT") {
          redactParseError(error);
          throw new Error(`Could not parse JSON config at ${filePath}; the existing file was left unchanged`, {
            cause: error,
          });
        }
      }
      if (!root || typeof root !== "object" || Array.isArray(root)) root = {};
      const currentServers = root[rootKey];
      const servers =
        currentServers && typeof currentServers === "object" && !Array.isArray(currentServers)
          ? { ...currentServers }
          : Object.create(null);
      Object.defineProperty(servers, name, { value: config, enumerable: true, configurable: true, writable: true });
      root[rootKey] = servers;
      await options.beforeWrite?.();
      await writePrivateFile(filePath, `${JSON.stringify(root, null, 2)}\n`, options);
    },
    options,
  );
}

function splitInputKeys(value) {
  const keys = [];
  for (let index = 0; index < value.length; index += 1) {
    const sequence = value.slice(index, index + 3);
    if (sequence === "\u001b[A" || sequence === "\u001b[B") {
      keys.push(sequence);
      index += 2;
      continue;
    }
    keys.push(value[index]);
  }
  return keys;
}

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
      await writePrivateFile(filePath, outputContent, options);
    },
    options,
  );
}

async function writePrivateFile(filePath, contents, options = {}) {
  await atomicWritePrivateFile(filePath, contents, options);
}

async function assertProjectConfigWritable(filePath, options = {}) {
  if (options.force) {
    return;
  }
  const tracked = await gitTrackedPath(filePath, options.projectDir || process.cwd());
  if (!tracked) {
    return;
  }
  throw new Error(
    [
      `Refusing to write AIPERMISSION_API_TOKEN into tracked git file: ${tracked}`,
      "Use --print to copy the config manually, untrack/ignore that file, or rerun with --force if you intentionally accept commit risk.",
    ].join("\n"),
  );
}

async function protectGitIgnoredConfig(filePath, startDir = process.cwd(), options = {}) {
  const repository = await discoverGitRepository(startDir);
  if (!repository) {
    return {};
  }
  const relativePath = path.relative(repository.workTree, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) {
    return {};
  }
  const excludePath = repository.excludePath;
  const temporaryRelativePath = path.relative(repository.workTree, privateTemporaryIgnorePath(filePath)).split(path.sep).join("/");
  const stagingRelativePath = path.relative(repository.workTree, privateStagingIgnorePath(filePath)).split(path.sep).join("/");
  const lockRelativePath = path.relative(repository.workTree, privateLockPath(filePath)).split(path.sep).join("/");
  const ignoreEntries = [
    gitIgnoreLiteral(relativePath),
    gitIgnoreWildcardPath(temporaryRelativePath),
    gitIgnoreWildcardPath(stagingRelativePath),
    gitIgnoreLiteral(lockRelativePath),
  ];
  try {
    await withPrivateFileLock(
      excludePath,
      async () => {
        let current = "";
        try {
          current = await fs.readFile(excludePath, "utf8");
        } catch (error) {
          if (error.code !== "ENOENT") throw error;
        }
        const entries = new Set(current.split(/\r?\n/));
        const missingEntries = ignoreEntries.filter((entry) => !entries.has(entry));
        if (missingEntries.length === 0) return;
        const prefix = current && !current.endsWith("\n") ? "\n" : "";
        await atomicWritePrivateFile(excludePath, `${current}${prefix}${missingEntries.join("\n")}\n`, {
          trustedRoot: path.dirname(excludePath),
        });
      },
      { trustedRoot: path.dirname(excludePath) },
    );
    await assertGitIgnored(repository, [
      ...(options.allowTracked ? [] : [relativePath]),
      privateTemporaryCheckPath(filePath, repository.workTree),
      privateStagingCheckPath(filePath, repository.workTree),
      lockRelativePath,
    ]);
  } catch (error) {
    throw new Error(`Could not protect MCP config with local Git excludes: ${error.message}`, { cause: error });
  }
  return {
    gitExcluded: true,
    gitExcludeEntry: relativePath,
    gitExcludeTemporaryEntry: temporaryRelativePath,
    gitExcludeStagingEntry: stagingRelativePath,
    gitExcludeLockEntry: lockRelativePath,
  };
}

export async function inspectProjectConfigProtection(filePath, startDir = process.cwd()) {
  const repository = await discoverGitRepository(startDir);
  if (!repository) return { repository: false };
  const relativePath = path.relative(repository.workTree, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) return { repository: false };
  const tracked = await gitTrackedPath(filePath, startDir);
  if (tracked) throw new Error(`MCP config is tracked by Git: ${tracked}`);
  await assertGitIgnored(repository, [
    relativePath,
    privateTemporaryCheckPath(filePath, repository.workTree),
    privateStagingCheckPath(filePath, repository.workTree),
    path.relative(repository.workTree, privateLockPath(filePath)).split(path.sep).join("/"),
  ]);
  return { repository: true, relativePath };
}

async function gitTrackedPath(filePath, startDir = process.cwd()) {
  const repository = await discoverGitRepository(startDir);
  if (!repository) {
    return "";
  }
  const relativePath = path.relative(repository.workTree, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) {
    return "";
  }
  try {
    await execFileAsync("git", ["-C", repository.workTree, "ls-files", "--error-unmatch", "--", relativePath], {
      windowsHide: true,
    });
    return relativePath;
  } catch (error) {
    if (error.code === 1) return "";
    throw new Error(`Could not verify whether MCP config is tracked by Git: ${gitErrorMessage(error)}`, { cause: error });
  }
}

async function discoverGitRepository(startDir) {
  try {
    const { stdout } = await execFileAsync("git", ["-C", path.resolve(startDir), "rev-parse", "--show-toplevel", "--absolute-git-dir"], {
      encoding: "utf8",
      windowsHide: true,
    });
    const [workTree, gitDir] = stdout.trim().split(/\r?\n/);
    if (!workTree || !gitDir) {
      return null;
    }
    const { stdout: excludeOutput } = await execFileAsync(
      "git",
      ["-C", path.resolve(startDir), "rev-parse", "--path-format=absolute", "--git-path", "info/exclude"],
      { encoding: "utf8", windowsHide: true },
    );
    const excludePath = excludeOutput.trim();
    if (!excludePath) throw new Error("Git did not return an exclude path");
    return { workTree: path.resolve(workTree), gitDir: path.resolve(gitDir), excludePath: path.resolve(excludePath) };
  } catch (error) {
    if (/not a git repository/i.test(`${error.stderr || ""}\n${error.message || ""}`)) return null;
    throw new Error(`Could not inspect Git repository: ${gitErrorMessage(error)}`, { cause: error });
  }
}

async function assertGitIgnored(repository, relativePaths) {
  for (const relativePath of relativePaths) {
    try {
      await execFileAsync("git", ["-C", repository.workTree, "check-ignore", "-q", "--", relativePath], {
        windowsHide: true,
      });
    } catch (error) {
      if (error.code === 1) {
        throw new Error(`Git still permits sensitive MCP path: ${relativePath}`, { cause: error });
      }
      throw new Error(`Could not verify local Git exclusion: ${gitErrorMessage(error)}`, { cause: error });
    }
  }
}

function privateTemporaryCheckPath(filePath, workTree) {
  return path.relative(workTree, privateTemporaryPath(filePath, "git-check")).split(path.sep).join("/");
}

function privateStagingCheckPath(filePath, workTree) {
  return path.relative(workTree, privateStagingPath(filePath, "git-check")).split(path.sep).join("/");
}

function escapeGitIgnoreFragment(value) {
  let result = "";
  for (const character of value) {
    result += ["\\", "*", "?", "[", "]", "#", "!", " "].includes(character) ? `\\${character}` : character;
  }
  return result;
}

function gitIgnoreLiteral(relativePath) {
  return `/${escapeGitIgnoreFragment(relativePath)}`;
}

function gitIgnoreWildcardPath(relativePath) {
  const wildcardIndex = relativePath.lastIndexOf("*");
  if (wildcardIndex < 0) throw new Error(`Git ignore wildcard path is missing its generated wildcard: ${relativePath}`);
  return `/${escapeGitIgnoreFragment(relativePath.slice(0, wildcardIndex))}*${escapeGitIgnoreFragment(
    relativePath.slice(wildcardIndex + 1),
  )}`;
}

function gitErrorMessage(error) {
  return String(error.stderr || error.message || error).trim();
}

function redactParseError(error, format = "JSON") {
  error.message = `${format} parsing failed`;
  error.stack = `${error.name || "Error"}: ${error.message}`;
  return error;
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
  if (state.multiline) {
    if (hasMultilineDelimiter(line, state.multiline)) state.multiline = "";
    return null;
  }
  const trimmed = line.trimStart();
  if (trimmed.startsWith("[") && !trimmed.startsWith("[[")) {
    try {
      return findMarkerPath(parseTOML(`${line}\n__aipermission_header_marker = true\n`));
    } catch {
      return null;
    }
  }
  for (const delimiter of ['"""', "'''"]) {
    if (!hasMultilineDelimiter(line, delimiter)) continue;
    if ((line.split(delimiter).length - 1) % 2 === 1) state.multiline = delimiter;
    break;
  }
  return null;
}

function hasMultilineDelimiter(line, delimiter) {
  const comment = line.indexOf("#");
  return (comment < 0 ? line : line.slice(0, comment)).includes(delimiter);
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
    redactParseError(error, "TOML");
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

function printProviderConfig(name, provider, apiUrl, target) {
  const baseConfig = buildMCPServerConfig({ apiUrl, token: "YOUR_TOKEN_HERE" });
  const previewConfig = provider === "custom" ? baseConfig : adaptMCPServerConfig(provider, baseConfig);
  console.log("");
  console.log(`${color.bold}${color.cyan}Copy-paste config:${color.reset}`);
  console.log("");
  if (target?.format === "toml") {
    console.log(tomlPreviewServerBlock(name, previewConfig));
    return;
  }
  console.log(JSON.stringify({ [target?.rootKey || "mcpServers"]: { [name]: previewConfig } }, null, 2));
}

// Keep stdout rendering separate from the secret-bearing TOML writer.
function tomlPreviewServerBlock(name, config) {
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

function printPlaceholderConfigNotice() {
  console.log("");
  console.log(`${color.yellow}Preview:${color.reset} the printed config uses YOUR_TOKEN_HERE and contains no bearer token.`);
  console.log(`${color.yellow}Replace the placeholder through the client's private environment or config mechanism.${color.reset}`);
}

export function sanitizeName(value) {
  const name = String(value || "")
    .trim()
    .replace(/[^a-zA-Z0-9_.-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!name) {
    throw new Error("MCP server name is required.");
  }
  if (["__proto__", "prototype", "constructor"].includes(name.toLowerCase())) {
    throw new Error("MCP server name is reserved.");
  }
  return name;
}

export function normalizeURL(value) {
  return normalizeLocalAPIURL(value || DEFAULT_API_URL);
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

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await runInit(process.argv.slice(2));
}
