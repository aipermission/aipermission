import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import readline from "node:readline/promises";
import { pathToFileURL } from "node:url";
import { stdin as input, stdout as output } from "node:process";
import { DEFAULT_API_URL, normalizeLocalAPIURL } from "./local-url.js";

const PACKAGE_NAME = "@aipermission/mcp";
const useColor = output.isTTY && !process.env.NO_COLOR;
const color = {
  reset: useColor ? "\x1b[0m" : "",
  bold: useColor ? "\x1b[1m" : "",
  dim: useColor ? "\x1b[2m" : "",
  green: useColor ? "\x1b[32m" : "",
  cyan: useColor ? "\x1b[36m" : "",
  yellow: useColor ? "\x1b[33m" : "",
};

const providers = [
  {
    id: "codex",
    label: "OpenAI Codex",
    description: "Writes ~/.codex/config.toml",
  },
  {
    id: "claude-code",
    label: "Claude Code",
    description: "Writes .mcp.json in the current project",
  },
  {
    id: "cursor",
    label: "Cursor",
    description: "Writes .cursor/mcp.json in the current project",
  },
  {
    id: "vscode",
    label: "VS Code",
    description: "Writes .vscode/mcp.json in the current project",
  },
  {
    id: "windsurf",
    label: "Windsurf",
    description: "Writes ~/.codeium/windsurf/mcp_config.json",
  },
  {
    id: "antigravity",
    label: "Google Antigravity",
    description: "Writes ~/.gemini/antigravity/mcp_config.json",
  },
  {
    id: "gemini",
    label: "Gemini CLI",
    description: "Writes ~/.gemini/settings.json",
  },
  {
    id: "custom",
    label: "Custom / copy-paste",
    description: "Prints config snippets only",
  },
];

export async function runInit(argv = []) {
  const flags = parseFlags(argv);
  const stdinToken = flags.tokenStdin ? (await readStdin()).trim() : "";
  const rl = readline.createInterface({ input, output });
  try {
    const provider = flags.provider
      ? findProvider(flags.provider)
      : await selectProvider("Which AI client should use this token?", providers);
    const name = sanitizeName(
      flags.name || (await ask(rl, "MCP server name", "aipermission"))
    );
    const apiUrl = normalizeURL(flags.apiUrl || DEFAULT_API_URL);
    const token = await resolveToken({ ...flags, stdinToken }, rl);

    if (!token) {
      throw new Error("API token is required.");
    }

    const config = buildMCPServerConfig({ apiUrl, token });
    if (provider.id === "custom" || flags.print) {
      printTokenConfigWarning();
      printCustomConfig(name, config);
      return;
    }

    const result = await writeProviderConfig(provider.id, name, config, { force: Boolean(flags.force) });
    console.log("");
    console.log(`${color.green}Configured ${provider.label}${color.reset}`);
    console.log(`${color.dim}Name:${color.reset} ${name}`);
    console.log(`${color.dim}Path:${color.reset} ${result.path}`);
    if (result.gitExcluded) {
      console.log(`${color.dim}Git:${color.reset} added ${result.gitExcludeEntry} to .git/info/exclude`);
    }
    console.log("");
    console.log(`${color.yellow}Keep this config private:${color.reset} it contains an AIPermission bearer token. If it is committed, revoke the token.`);
    console.log(`${color.yellow}Restart the AI client so it reloads MCP servers.${color.reset}`);
  } finally {
    rl.close();
  }
}

export function parseFlags(argv) {
  const result = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (!arg.startsWith("--")) {
      continue;
    }
    const [rawKey, inlineValue] = arg.slice(2).split("=", 2);
    const key = rawKey.replace(/-([a-z])/g, (_, letter) => letter.toUpperCase());
    if (key === "print") {
      result.print = true;
      continue;
    }
    if (key === "force") {
      result.force = true;
      continue;
    }
    if (key === "tokenStdin") {
      result.tokenStdin = true;
      continue;
    }
    if (key === "token") {
      throw new Error("--token is not supported; use the hidden prompt or --token-stdin");
    }
    result[key] = inlineValue ?? argv[i + 1] ?? "";
    if (inlineValue === undefined) {
      i += 1;
    }
  }
  return result;
}

function findProvider(idOrLabel) {
  const normalized = String(idOrLabel).trim().toLowerCase();
  const provider = providers.find(
    (item) => item.id === normalized || item.label.toLowerCase() === normalized
  );
  if (!provider) {
    throw new Error(`Unknown provider: ${idOrLabel}`);
  }
  return provider;
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
    const lines = [
      `${color.bold}${color.cyan}${title}${color.reset}`,
      `${color.dim}Use ↑/↓ and Enter.${color.reset}`,
      "",
    ];
    for (let i = 0; i < items.length; i += 1) {
      const selected = i === index;
      const marker = selected ? `${color.green}›${color.reset}` : " ";
      const label = selected ? `${color.bold}${items[i].label}${color.reset}` : items[i].label;
      lines.push(
        `${marker} ${label} ${color.dim}- ${items[i].description}${color.reset}`
      );
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
      const keys = value.match(/\u001b\[[AB]|\r|\n|\u0003|./g) || [];
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
    args: ["-y", PACKAGE_NAME],
    env: {
      NODE_ENV: "production",
      AIPERMISSION_API_URL: apiUrl,
      AIPERMISSION_API_TOKEN: token,
    },
  };
}

export async function writeProviderConfig(providerID, name, config, options = {}) {
  if (providerID === "codex") {
    const filePath = path.join(os.homedir(), ".codex", "config.toml");
    await writeCodexConfig(filePath, name, config);
    return { path: filePath };
  }
  if (providerID === "claude-code") {
    const filePath = path.join(process.cwd(), ".mcp.json");
    await assertProjectConfigWritable(filePath, options);
    await writeJSONMCPConfig(filePath, name, config, "mcpServers");
    return { path: filePath, ...(await protectGitIgnoredConfig(filePath)) };
  }
  if (providerID === "cursor") {
    const filePath = path.join(process.cwd(), ".cursor", "mcp.json");
    await assertProjectConfigWritable(filePath, options);
    await writeJSONMCPConfig(filePath, name, config, "mcpServers");
    return { path: filePath, ...(await protectGitIgnoredConfig(filePath)) };
  }
  if (providerID === "vscode") {
    const filePath = path.join(process.cwd(), ".vscode", "mcp.json");
    await assertProjectConfigWritable(filePath, options);
    await writeJSONMCPConfig(filePath, name, config, "servers");
    return { path: filePath, ...(await protectGitIgnoredConfig(filePath)) };
  }
  if (providerID === "windsurf") {
    const filePath = path.join(os.homedir(), ".codeium", "windsurf", "mcp_config.json");
    await writeJSONMCPConfig(filePath, name, config, "mcpServers");
    return { path: filePath };
  }
  if (providerID === "antigravity") {
    const filePath = path.join(os.homedir(), ".gemini", "antigravity", "mcp_config.json");
    await writeJSONMCPConfig(filePath, name, config, "mcpServers");
    return { path: filePath };
  }
  if (providerID === "gemini") {
    const filePath = path.join(os.homedir(), ".gemini", "settings.json");
    await writeJSONMCPConfig(filePath, name, config, "mcpServers");
    return { path: filePath };
  }
  throw new Error(`Unsupported provider: ${providerID}`);
}

export async function writeJSONMCPConfig(filePath, name, config, rootKey) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  let root = {};
  try {
    root = JSON.parse(await fs.readFile(filePath, "utf8"));
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw new Error(`Could not read JSON config at ${filePath}: ${error.message}`);
    }
  }
  if (!root || typeof root !== "object" || Array.isArray(root)) {
    root = {};
  }
  root[rootKey] =
    root[rootKey] && typeof root[rootKey] === "object" && !Array.isArray(root[rootKey])
      ? root[rootKey]
      : {};
  root[rootKey][name] = config;
  await writePrivateFile(filePath, `${JSON.stringify(root, null, 2)}\n`);
}

async function writeCodexConfig(filePath, name, config) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  let current = "";
  try {
    current = await fs.readFile(filePath, "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") {
      throw error;
    }
  }

  const next = removeCodexServer(current, name).trimEnd();
  const block = codexServerBlock(name, config);
  await writePrivateFile(filePath, `${next ? `${next}\n\n` : ""}${block}\n`);
}

async function writePrivateFile(filePath, contents) {
  await fs.writeFile(filePath, contents, { mode: 0o600 });
  await fs.chmod(filePath, 0o600);
}

async function assertProjectConfigWritable(filePath, options = {}) {
  if (options.force) {
    return;
  }
  const tracked = await gitTrackedPath(filePath);
  if (!tracked) {
    return;
  }
  throw new Error(
    [
      `Refusing to write AIPERMISSION_API_TOKEN into tracked git file: ${tracked}`,
      "Use --print to copy the config manually, untrack/ignore that file, or rerun with --force if you intentionally accept commit risk.",
    ].join("\n")
  );
}

async function protectGitIgnoredConfig(filePath) {
  const gitRoot = await findGitRoot(process.cwd());
  if (!gitRoot) {
    return {};
  }
  const relativePath = path.relative(gitRoot, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) {
    return {};
  }
  const excludePath = path.join(gitRoot, ".git", "info", "exclude");
  let current = "";
  try {
    current = await fs.readFile(excludePath, "utf8");
  } catch (error) {
    if (error.code !== "ENOENT") {
      return {};
    }
  }
  const entries = current.split(/\r?\n/).map((line) => line.trim());
  if (!entries.includes(relativePath)) {
    const prefix = current && !current.endsWith("\n") ? "\n" : "";
    try {
      await fs.mkdir(path.dirname(excludePath), { recursive: true });
      await fs.appendFile(excludePath, `${prefix}${relativePath}\n`, { mode: 0o600 });
    } catch {
      return {};
    }
  }
  return { gitExcluded: true, gitExcludeEntry: relativePath };
}

async function gitTrackedPath(filePath) {
  const gitRoot = await findGitRoot(process.cwd());
  if (!gitRoot) {
    return "";
  }
  const relativePath = path.relative(gitRoot, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) {
    return "";
  }
  const gitDir = path.join(gitRoot, ".git");
  const args = ["--git-dir", gitDir, "--work-tree", gitRoot, "ls-files", "--error-unmatch", "--", relativePath];
  try {
    const { spawn } = await import("node:child_process");
    await new Promise((resolve, reject) => {
      const child = spawn("git", args, { stdio: "ignore" });
      child.on("error", reject);
      child.on("exit", (code) => {
        if (code === 0) {
          resolve();
          return;
        }
        reject(Object.assign(new Error("not tracked"), { code }));
      });
    });
    return relativePath;
  } catch {
    return "";
  }
}

async function findGitRoot(startDir) {
  let current = path.resolve(startDir);
  for (;;) {
    const gitPath = path.join(current, ".git");
    try {
      const stat = await fs.stat(gitPath);
      if (stat.isDirectory()) {
        return current;
      }
    } catch (error) {
      if (error.code !== "ENOENT") {
        return null;
      }
    }
    const parent = path.dirname(current);
    if (parent === current) {
      return null;
    }
    current = parent;
  }
}

function removeCodexServer(source, name) {
  const main = `[mcp_servers.${tomlKey(name)}]`;
  const env = `[mcp_servers.${tomlKey(name)}.env]`;
  const lines = source.split(/\r?\n/);
  const kept = [];
  let skipping = false;
  for (const line of lines) {
    const trimmed = line.trim();
    const isHeader = trimmed.startsWith("[") && trimmed.endsWith("]");
    if (trimmed === main || trimmed === env) {
      skipping = true;
      continue;
    }
    if (isHeader && skipping) {
      skipping = false;
    }
    if (!skipping) {
      kept.push(line);
    }
  }
  return kept.join("\n");
}

function codexServerBlock(name, config) {
  return `[mcp_servers.${tomlKey(name)}]
command = ${tomlString(config.command)}
args = [${config.args.map(tomlString).join(", ")}]
enabled = true

[mcp_servers.${tomlKey(name)}.env]
NODE_ENV = ${tomlString(config.env.NODE_ENV)}
AIPERMISSION_API_URL = ${tomlString(config.env.AIPERMISSION_API_URL)}
AIPERMISSION_API_TOKEN = ${tomlString(config.env.AIPERMISSION_API_TOKEN)}`;
}

function printCustomConfig(name, config) {
  console.log("");
  console.log(`${color.bold}${color.cyan}Copy-paste config:${color.reset}`);
  console.log("");
  console.log(JSON.stringify({ mcpServers: { [name]: config } }, null, 2));
}

function printTokenConfigWarning() {
  console.log("");
  console.log(`${color.yellow}Warning:${color.reset} the printed config contains an AIPermission bearer token.`);
  console.log(`${color.yellow}Keep it private and revoke the token if it is shared or committed.${color.reset}`);
}

export function sanitizeName(value) {
  const name = String(value || "")
    .trim()
    .replace(/[^a-zA-Z0-9_.-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  if (!name) {
    throw new Error("MCP server name is required.");
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

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await runInit(process.argv.slice(2));
}
