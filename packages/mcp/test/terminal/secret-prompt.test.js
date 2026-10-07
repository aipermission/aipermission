import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { parse } from "smol-toml";

const ptyProbe = String.raw`
import errno, json, os, pty, select, subprocess, sys, termios, time
master, slave = pty.openpty()
initial = termios.tcgetattr(slave)
process = subprocess.Popen([sys.argv[1], sys.argv[2], "init", "--provider", "codex", "--name", "terminal-test", "--home", sys.argv[3]], stdin=slave, stdout=slave, stderr=slave)
captured = b""
sent = False
deadline = time.monotonic() + 10
try:
    while time.monotonic() < deadline:
        if select.select([master], [], [], 0.1)[0]:
            try:
                part = os.read(master, 65536)
            except OSError as error:
                if error.errno == errno.EIO:
                    break
                raise
            captured += part
            if not sent and b"API token:" in captured:
                if sys.argv[4] == "cancel":
                    os.write(master, b"\x03")
                else:
                    os.write(master, b"SYNTHETIC_SECRET_CANARY\xf0\x9f")
                    time.sleep(0.03)
                    os.write(master, b"\x98\x80\x7fZ\r")
                sent = True
        if process.poll() is not None:
            break
    status = process.wait(timeout=2)
    while select.select([master], [], [], 0.1)[0]:
        part = os.read(master, 65536)
        if not part:
            break
        captured += part
    final = termios.tcgetattr(slave)
    print(json.dumps({"status": status, "output": captured.decode("utf8", "replace"), "sent": sent, "terminal_restored": initial == final}))
finally:
    if process.poll() is None:
        process.kill()
        process.wait()
    os.close(master)
    os.close(slave)
`;

test("interactive CLI hides token bytes and restores terminal on success and Ctrl+C", async (t) => {
  if (process.platform === "win32") {
    // POSIX PTY behavior is verified on Linux/macOS; Windows ACL tests remain mandatory.
    return;
  }
  const home = await fs.mkdtemp(path.join(os.tmpdir(), "aipermission-secret-pty-"));
  t.after(() => fs.rm(home, { recursive: true, force: true }));
  for (const mode of ["success", "cancel", "cancel-fresh"]) {
    const targetHome = mode === "cancel-fresh" ? path.join(home, "fresh") : home;
    const configPath = path.join(targetHome, ".codex", "config.toml");
    const before = await configBytes(configPath);
    assert.equal(before === null, mode !== "cancel");
    const result = spawnSync(
      "python3",
      ["-c", ptyProbe, process.execPath, path.resolve("src/cli.js"), targetHome, mode === "success" ? "success" : "cancel"],
      {
        encoding: "utf8",
        timeout: 15000,
      },
    );
    assert.equal(result.status, 0, result.stderr || String(result.error));
    const probe = JSON.parse(result.stdout);
    assert.equal(probe.sent, true);
    assert.equal(probe.status, mode === "success" ? 0 : 130, probe.output);
    assert.equal(probe.terminal_restored, true);
    assert.doesNotMatch(probe.output, /SYNTHETIC|SECRET|CANARY/);
    if (mode === "success") {
      const config = await fs.readFile(configPath, "utf8");
      assert.equal(parse(config).mcp_servers["terminal-test"].env.AIPERMISSION_API_TOKEN, "SYNTHETIC_SECRET_CANARYZ");
    } else assert.deepEqual(await configBytes(configPath), before);
  }
});

async function configBytes(filename) {
  try {
    return await fs.readFile(filename);
  } catch (error) {
    if (error.code !== "ENOENT") throw error;
    return null;
  }
}
