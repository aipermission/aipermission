import { execFile } from "node:child_process";
import path from "node:path";
import { promisify } from "node:util";
import { readPrivateFileSnapshot } from "./private-file-snapshot.js";
import { privateRecoveryDirectory } from "./private-file-recovery.js";
import {
  atomicWritePrivateFile,
  privateLockPath,
  privateStagingIgnorePath,
  privateStagingPath,
  privateTemporaryIgnorePath,
  privateTemporaryPath,
  withPrivateFileLock,
} from "./private-file.js";

const execFileAsync = promisify(execFile);

export async function assertProjectConfigWritable(filePath, options = {}) {
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

export async function protectGitIgnoredConfig(filePath, startDir = process.cwd(), options = {}) {
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
  const recoveryRelativePath = path.relative(repository.workTree, privateRecoveryDirectory(filePath)).split(path.sep).join("/");
  if (await gitTrackedPath(privateRecoveryDirectory(filePath), startDir)) throw new Error("Refusing tracked MCP config recovery files");
  const ignoreEntries = [
    gitIgnoreLiteral(relativePath),
    gitIgnoreWildcardPath(temporaryRelativePath),
    gitIgnoreWildcardPath(stagingRelativePath),
    gitIgnoreLiteral(lockRelativePath),
    gitIgnoreLiteral(recoveryRelativePath),
  ];
  try {
    await withPrivateFileLock(
      excludePath,
      async () => {
        const snapshot = await readPrivateFileSnapshot(excludePath);
        const current = snapshot.content;
        const entries = new Set(current.split(/\r?\n/));
        const missingEntries = ignoreEntries.filter((entry) => !entries.has(entry));
        if (missingEntries.length === 0) return;
        const prefix = current && !current.endsWith("\n") ? "\n" : "";
        await options.beforeWrite?.();
        await atomicWritePrivateFile(excludePath, `${current}${prefix}${missingEntries.join("\n")}\n`, {
          trustedRoot: path.dirname(excludePath),
          expectedSnapshot: snapshot,
        });
      },
      { trustedRoot: path.dirname(excludePath) },
    );
    await assertGitIgnored(repository, [
      ...(options.allowTracked ? [] : [relativePath]),
      privateTemporaryCheckPath(filePath, repository.workTree),
      privateStagingCheckPath(filePath, repository.workTree),
      lockRelativePath,
      `${recoveryRelativePath}/previous`,
      `${recoveryRelativePath}/candidate`,
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
    gitExcludeRecoveryEntry: recoveryRelativePath,
  };
}

export async function inspectProjectConfigProtection(filePath, startDir = process.cwd()) {
  const repository = await discoverGitRepository(startDir);
  if (!repository) return { repository: false };
  const relativePath = path.relative(repository.workTree, filePath).split(path.sep).join("/");
  if (relativePath.startsWith("../") || path.isAbsolute(relativePath)) return { repository: false };
  const tracked = await gitTrackedPath(filePath, startDir);
  if (tracked) throw new Error(`MCP config is tracked by Git: ${tracked}`);
  if (await gitTrackedPath(privateRecoveryDirectory(filePath), startDir)) throw new Error("MCP config recovery files are tracked by Git");
  await assertGitIgnored(repository, [
    relativePath,
    privateTemporaryCheckPath(filePath, repository.workTree),
    privateStagingCheckPath(filePath, repository.workTree),
    path.relative(repository.workTree, privateLockPath(filePath)).split(path.sep).join("/"),
    path
      .relative(repository.workTree, path.join(privateRecoveryDirectory(filePath), "previous"))
      .split(path.sep)
      .join("/"),
    path
      .relative(repository.workTree, path.join(privateRecoveryDirectory(filePath), "candidate"))
      .split(path.sep)
      .join("/"),
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
    await execFileAsync("git", ["-C", repository.workTree, "ls-files", "--error-unmatch", "--", `:(literal,icase)${relativePath}`], {
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
