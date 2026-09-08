import { execFileSync, spawnSync } from "node:child_process";

export function findChangedOwnerEntries(repositoryRoot, ref, isBehaviorOwner) {
  verifyCommit(repositoryRoot, ref);
  const tracked = execFileSync("git", ["diff", "--name-status", "--diff-filter=ACMR", ref, "--", "frontend/src"], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  const untracked = execFileSync("git", ["ls-files", "--others", "--exclude-standard", "--", "frontend/src"], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  const entries = new Map();
  for (const line of tracked.trim().split("\n").filter(Boolean)) {
    const [status, ...paths] = line.split("\t");
    const file = normalizeFrontendPath(paths.at(-1));
    if (isBehaviorOwner(file)) entries.set(file, { file, status: status[0], untracked: false });
  }
  for (const path of untracked.trim().split("\n").filter(Boolean)) {
    const file = normalizeFrontendPath(path);
    if (isBehaviorOwner(file)) entries.set(file, { file, status: "A", untracked: true });
  }
  return [...entries.values()].sort((left, right) => left.file.localeCompare(right.file));
}

export function readBaselineAt(repositoryRoot, ref) {
  verifyCommit(repositoryRoot, ref);
  const object = `${ref}:frontend/.changed-coverage-baseline.json`;
  const listing = spawnSync("git", ["ls-tree", "-r", "--name-only", ref, "--", "frontend/.changed-coverage-baseline.json"], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (listing.error) throw listing.error;
  if (listing.status !== 0) throw new Error(`Cannot inspect changed coverage baseline at ${ref}: ${listing.stderr.trim()}`);
  if (!listing.stdout.trim()) return null;
  const result = spawnSync("git", ["show", object], { cwd: repositoryRoot, encoding: "utf8" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Cannot read changed coverage baseline from ${ref}: ${result.stderr.trim()}`);
  try {
    return JSON.parse(result.stdout);
  } catch (error) {
    throw new Error(`Changed coverage baseline at ${ref} is invalid JSON: ${error.message}`, { cause: error });
  }
}

export function resolveBootstrapRevision(repositoryRoot, { revision, tree, head = "HEAD" }) {
  if (!/^[a-f0-9]{40,64}$/.test(revision || "")) throw new Error("Coverage bootstrap_revision must be a full Git object ID");
  if (!/^[a-f0-9]{40,64}$/.test(tree || "")) throw new Error("Coverage bootstrap_tree must be a full Git object ID");
  verifyCommit(repositoryRoot, head);

  const history = spawnSync("git", ["log", "--format=%H%x09%T", head], { cwd: repositoryRoot, encoding: "utf8" });
  if (history.error) throw history.error;
  if (history.status !== 0) throw new Error(`Cannot inspect coverage bootstrap history: ${history.stderr.trim()}`);
  const candidates = history.stdout
    .trim()
    .split("\n")
    .filter(Boolean)
    .map((line) => line.split("\t"))
    .filter(([, candidateTree]) => candidateTree === tree)
    .map(([commit]) => commit);
  if (candidates.length === 0) {
    throw new Error(`Coverage bootstrap tree ${tree} is not reachable from ${head}`);
  }
  return candidates.includes(revision) ? revision : candidates[0];
}

function verifyCommit(repositoryRoot, ref) {
  const result = spawnSync("git", ["rev-parse", "--verify", `${ref}^{commit}`], {
    cwd: repositoryRoot,
    encoding: "utf8",
  });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`Changed coverage base is not a valid commit: ${ref}`);
}

function normalizeFrontendPath(file) {
  return String(file || "").replace(/^frontend\//, "");
}
