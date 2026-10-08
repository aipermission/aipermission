import { createRequire } from "node:module";
import readline from "node:readline/promises";
import { pathToFileURL } from "node:url";
import { stdin as input, stdout as output } from "node:process";
import { StringDecoder } from "node:string_decoder";
import { parseCommandFlags } from "./cli-flags.js";
import { DEFAULT_API_URL, normalizeLocalAPIURL } from "./local-url.js";
import { adaptMCPServerConfig, getClient, MCP_PROVIDERS, resolveMCPConfigTarget, resolveMCPPrintTarget } from "./client-registry.js";
import { commitSkillInstallation, prepareSkillInstallation } from "./install-skill.js";
import { writeProviderConfig } from "./init-config.js";
import { tomlPreviewServerBlock } from "./init-toml.js";

export { writeJSONMCPConfig, writeProviderConfig } from "./init-config.js";
export { inspectProjectConfigProtection } from "./init-git.js";
export { tomlKey, tomlString, writeTOMLMCPConfig } from "./init-toml.js";

const require = createRequire(import.meta.url);
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
  let rl;
  const getReadline = () => {
    rl ||= readline.createInterface({ input, output });
    return rl;
  };
  try {
    const provider = flags.provider
      ? findProvider(flags.provider)
      : await selectProvider("Which AI client should use this token?", MCP_PROVIDERS);
    const needsToken = provider.id !== "custom" && !flags.print;
    const stdinToken = needsToken && flags.tokenStdin ? (await readStdin()).trim() : "";
    const name = sanitizeName(flags.name || (interactive ? await ask(getReadline(), "MCP server name", "aipermission") : "aipermission"));
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

    const token = flags.tokenStdin ? stdinToken : await resolveToken(flags, getReadline());
    if (!token) {
      throw new Error("API token is required.");
    }
    const config = adaptMCPServerConfig(provider.id, buildMCPServerConfig({ apiUrl, token }));
    const result = await writeProviderConfig(provider.id, name, config, {
      force: Boolean(flags.force),
      scope: flags.scope,
      homeDir: flags.home,
      projectDir: flags.projectDir,
    });
    let skillResult;
    if (preparedSkill) {
      try {
        skillResult = await reportInstalledSkill(preparedSkill);
      } catch (error) {
        throw new Error(
          `MCP config was written at ${result.path}, but operator skill installation failed. Complete the skill installation separately.`,
          {
            cause: error,
          },
        );
      }
    }
    console.log("");
    console.log(`${color.green}Configured ${provider.label}${color.reset}`);
    console.log(`${color.dim}Name:${color.reset} ${name}`);
    console.log(`${color.dim}Path:${color.reset} ${result.path}`);
    if (result.recoveryPath)
      console.log(`${color.dim}Previous config recovery (may contain secrets):${color.reset} ${result.recoveryPath}`);
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
    rl?.close();
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

  // Closing detaches readline's keypress echo handler before raw secret input.
  rl.close();
  input.setRawMode(true);
  input.resume();

  let value = "";
  const decoder = new StringDecoder("utf8");
  return await new Promise((resolve, reject) => {
    const cleanup = () => {
      input.off("data", onData);
      input.off("end", onEnd);
      input.off("error", onError);
      input.setRawMode(false);
      input.pause();
      output.write("\n");
    };
    const onEnd = () => {
      cleanup();
      reject(new Error("Token input ended before confirmation."));
    };
    const onError = (error) => {
      cleanup();
      reject(error);
    };
    const onData = (buffer) => {
      const text = decoder.write(buffer);
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
          value = value.replace(/.$/u, "");
          continue;
        }
        value += char;
      }
    };
    input.on("data", onData);
    input.once("end", onEnd);
    input.once("error", onError);
    output.write(`${label}: `);
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

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await runInit(process.argv.slice(2));
}
