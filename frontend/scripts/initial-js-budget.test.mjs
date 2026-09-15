import assert from "node:assert/strict";
import { test } from "node:test";
import { initialJavaScriptFiles } from "./initial-js-budget.mjs";

test("measures only eager JavaScript, not dynamically imported terminal chunks", () => {
  const manifest = {
    "index.html": { isEntry: true, file: "assets/index.js", imports: ["_react.js"], dynamicImports: ["terminal.jsx"] },
    "_react.js": { file: "assets/react.js" },
    "terminal.jsx": { file: "assets/terminal.js", imports: ["_react.js"] },
  };
  assert.deepEqual(initialJavaScriptFiles(manifest).sort(), ["assets/index.js", "assets/react.js"]);
});

test("rejects broken eager-import manifest edges", () => {
  assert.throws(
    () => initialJavaScriptFiles({ "index.html": { isEntry: true, file: "assets/index.js", imports: ["missing"] } }),
    /missing/,
  );
});
