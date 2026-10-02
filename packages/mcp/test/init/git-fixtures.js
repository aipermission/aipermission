import { execFile } from "node:child_process";
import { promisify } from "node:util";

const execFileAsync = promisify(execFile);

export async function git(cwd, ...args) {
  const { stdout } = await execFileAsync("git", ["-C", cwd, ...args], { encoding: "utf8", windowsHide: true });
  return stdout.trim();
}

export async function initGitRepository(dir) {
  await git(dir, "init");
  await git(dir, "config", "user.name", "AIPermission Tests");
  await git(dir, "config", "user.email", "tests@aipermission.local");
}
