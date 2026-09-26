import { describe, expect, it } from "vitest";
import { sshDockerResponse } from "./operation-contracts";

describe("SSH Docker operation contracts", () => {
  it("validates status and log output without treating absent metadata as present", () => {
    expect(sshDockerResponse({ ok: true })).toEqual({ ok: true });
    const value = {
      ok: true,
      available: true,
      duration_ms: 3,
      exit_code: 0,
      containers: [{ id: "fixture", name: "api", ports: "80/tcp" }],
    };
    expect(sshDockerResponse(value)).toEqual(value);
    expect(sshDockerResponse({ ok: false, stderr: "denied", exit_code: 1 })).toMatchObject({ ok: false, stderr: "denied" });
  });

  it("rejects malformed status, logs, timing, and container fields", () => {
    for (const value of [
      null,
      [],
      {},
      { ok: "true" },
      { ok: true, available: 1 },
      { ok: true, stdout: {} },
      { ok: true, duration_ms: -1 },
      { ok: true, exit_code: "0" },
      { ok: true, containers: {} },
      { ok: true, containers: [null] },
      { ok: true, containers: [{ ports: {} }] },
    ]) {
      expect(() => sshDockerResponse(value)).toThrow("Invalid SSH Docker operation response.");
    }
  });
});
