import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export function createCoverageReportDirectory() {
  const path = mkdtempSync(join(tmpdir(), "aipermission-changed-coverage-"));
  return {
    path,
    cleanup() {
      rmSync(path, { force: true, recursive: true });
    },
  };
}
