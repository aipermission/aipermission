import assert from "node:assert/strict";
import test from "node:test";
import { spawnSync } from "node:child_process";
import { formatRelativeDeadline, toLocalDateTime, toRFC3339 } from "../date-time.ts";

test("date helpers normalize editor and API timestamps", () => {
  assert.equal(toRFC3339("2026-07-30T09:15"), new Date("2026-07-30T09:15").toISOString());
  const instant = new Date("2026-07-30T06:15:00Z");
  const local = new Date(instant.getTime() - instant.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  assert.equal(toLocalDateTime(instant.toISOString()), local);
  assert.equal(toRFC3339("not-a-date"), "");
  assert.equal(toLocalDateTime("not-a-date"), "");
});

test("local editor timestamps preserve calendar boundaries across timezones", () => {
  const helperURL = new URL("../date-time.ts", import.meta.url).href;
  for (const [zone, local] of [
    ["UTC", "2026-07-30T06:15"],
    ["Europe/Istanbul", "2026-07-30T09:15"],
    ["America/Los_Angeles", "2026-07-29T23:15"],
  ]) {
    const script = `import assert from 'node:assert/strict';
      import { toLocalDateTime, toRFC3339 } from ${JSON.stringify(helperURL)};
      assert.equal(toLocalDateTime('2026-07-30T06:15:00Z'), ${JSON.stringify(local)});
      assert.equal(toRFC3339(${JSON.stringify(local)}), '2026-07-30T06:15:00.000Z');`;
    const result = spawnSync(process.execPath, ["--experimental-strip-types", "--input-type=module", "-e", script], {
      env: { ...process.env, TZ: zone },
      encoding: "utf8",
      timeout: 10000,
    });
    assert.equal(result.status, 0, `${zone}: ${result.error || result.stderr}`);
  }
});

test("relative deadline reports expired values", () => {
  assert.equal(formatRelativeDeadline("2000-01-01T00:00:00Z"), "expired");
});
