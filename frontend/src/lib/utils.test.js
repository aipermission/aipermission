import assert from "node:assert/strict";
import test from "node:test";

import { cn } from "./utils.js";

test("cn merges conditional classes", () => {
  const showHiddenClass = false;
  assert.equal(cn("px-2", showHiddenClass && "hidden", ["text-sm", "font-semibold"]), "px-2 text-sm font-semibold");
});

test("cn resolves Tailwind conflicts", () => {
  assert.equal(cn("px-2", "px-4"), "px-4");
});
