import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const srcRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const publicRoot = path.resolve(srcRoot, "../public");
const allowedInlineStyleFile = "components/history/history-components.jsx";

test("first-party inline styles stay limited to validated history label colors", () => {
  const occurrences = [];
  for (const file of sourceFiles(srcRoot)) {
    const relative = path.relative(srcRoot, file);
    const source = fs.readFileSync(file, "utf8");
    const count = [...source.matchAll(/\bstyle\s*=\s*\{/g)].length;
    if (count > 0) occurrences.push({ relative, count });
    assert.doesNotMatch(source, /document\.documentElement\.style\./, `${relative} mutates an inline root style`);
  }
  assert.deepEqual(occurrences, [{ relative: allowedInlineStyleFile, count: 2 }]);
});

test("public initialization scripts do not create inline styles", () => {
  for (const file of sourceFiles(publicRoot)) {
    const relative = path.relative(publicRoot, file);
    const source = fs.readFileSync(file, "utf8");
    assert.doesNotMatch(source, /\bstyle\s*=\s*\{|\.style\.|setAttribute\(["']style["']/, `${relative} creates an inline style`);
  }
});

function sourceFiles(directory) {
  return fs.readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const file = path.join(directory, entry.name);
    if (entry.isDirectory()) return sourceFiles(file);
    return /\.(?:js|jsx)$/.test(entry.name) ? [file] : [];
  });
}
