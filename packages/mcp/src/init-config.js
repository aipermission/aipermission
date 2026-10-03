import { adaptMCPServerConfig, resolveMCPConfigTarget } from "./client-registry.js";
import { assertProjectConfigWritable, protectGitIgnoredConfig } from "./init-git.js";
import { writeTOMLMCPConfig } from "./init-toml.js";
import { updateJSONCServer } from "./jsonc-config.js";
import { atomicWritePrivateFile, withPrivateFileLock } from "./private-file.js";
import { readPrivateFileSnapshot } from "./private-file-snapshot.js";

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
    jsonc: providerID === "vscode",
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
      let snapshot;
      try {
        snapshot = await readPrivateFileSnapshot(filePath);
      } catch (error) {
        throw new Error(`Could not read JSON config at ${filePath}; the existing file was left unchanged`, { cause: error });
      }
      const content = snapshot.content;
      let outputContent;
      if (options.jsonc) {
        outputContent = updateJSONCServer(content, rootKey, name, config);
      } else {
        let root;
        try {
          root = content ? JSON.parse(content) : {};
        } catch (error) {
          redactParseError(error);
          throw new Error(`Could not parse JSON config at ${filePath}; the existing file was left unchanged`, { cause: error });
        }
        if (!root || typeof root !== "object" || Array.isArray(root)) root = {};
        const currentServers = root[rootKey];
        const servers =
          currentServers && typeof currentServers === "object" && !Array.isArray(currentServers)
            ? { ...currentServers }
            : Object.create(null);
        Object.defineProperty(servers, name, { value: config, enumerable: true, configurable: true, writable: true });
        root[rootKey] = servers;
        outputContent = `${JSON.stringify(root, null, 2)}\n`;
      }
      await options.beforeWrite?.();
      await atomicWritePrivateFile(filePath, outputContent, { ...options, expectedSnapshot: snapshot });
    },
    options,
  );
}

function redactParseError(error, format = "JSON") {
  error.message = `${format} parsing failed`;
  error.stack = `${error.name || "Error"}: ${error.message}`;
  return error;
}
