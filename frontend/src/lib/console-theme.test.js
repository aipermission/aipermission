import test from "node:test";
import assert from "node:assert/strict";
import { connectorConsoleTheme } from "../connectors/templates/_shared/console-theme.js";

test("connector console theme exposes stable light and dark tokens", () => {
  const light = connectorConsoleTheme("light");
  const dark = connectorConsoleTheme("dark");

  assert.match(light.panel, /bg-white/);
  assert.match(light.input, /placeholder:text-stone-400/);
  assert.match(dark.panel, /bg-\[#1e1e1e\]/);
  assert.match(dark.activeRow, /border-emerald-700/);
});
