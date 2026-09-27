import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import { expect, it, vi } from "vitest";
import { applySQLEditorTheme, loadSQLMonaco } from "../../connectors/templates/_shared/sql-editor-runtime";

vi.mock("monaco-editor/esm/vs/editor/editor.api", () => ({
  editor: { defineTheme: vi.fn(), setTheme: vi.fn() },
}));
vi.mock("monaco-editor/esm/vs/editor/editor.worker?worker", () => ({ default: class SQLWorker {} }));
vi.mock("monaco-editor/esm/vs/basic-languages/sql/sql.contribution", () => ({}));
vi.mock("monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController.js", () => ({}));

it("shares one pending Monaco load and installs its SQL worker", async () => {
  const previousEnvironment = window.MonacoEnvironment;
  try {
    const first = loadSQLMonaco();
    expect(loadSQLMonaco()).toBe(first);
    expect(await first).toBe(monaco);
    expect(loadSQLMonaco()).toBe(first);
    expect(window.MonacoEnvironment?.getWorker?.()).toBeInstanceOf(Object);
  } finally {
    window.MonacoEnvironment = previousEnvironment;
  }
});

it("retries a failed worker-module load instead of caching the rejected promise", async () => {
  vi.resetModules();
  vi.doMock("monaco-editor/esm/vs/editor/editor.worker?worker", () => {
    throw new Error("Worker module unavailable");
  });
  const previousEnvironment = window.MonacoEnvironment;
  try {
    const runtime = await import("../../connectors/templates/_shared/sql-editor-runtime");
    await expect(runtime.loadSQLMonaco()).rejects.toThrow();
    vi.doMock("monaco-editor/esm/vs/editor/editor.worker?worker", () => ({ default: class RecoveredSQLWorker {} }));
    await expect(runtime.loadSQLMonaco()).resolves.toHaveProperty("editor");
    expect(window.MonacoEnvironment?.getWorker?.()).toBeInstanceOf(Object);
  } finally {
    vi.doMock("monaco-editor/esm/vs/editor/editor.worker?worker", () => ({ default: class SQLWorker {} }));
    window.MonacoEnvironment = previousEnvironment;
    vi.resetModules();
  }
});

it.each([
  { theme: "light", name: "aipermission-sql-light", base: "vs", background: "#fafaf9", suggestions: "#ffffff" },
  { theme: "dark", name: "aipermission-sql-dark", base: "vs-dark", background: "#252526", suggestions: "#252526" },
])("applies the $theme SQL editor palette without line highlight borders", ({ theme, name, base, background, suggestions }) => {
  expect(applySQLEditorTheme(monaco, theme)).toBe(name);
  expect(monaco.editor.defineTheme).toHaveBeenCalledWith(
    name,
    expect.objectContaining({
      base,
      inherit: true,
      rules: [],
      colors: expect.objectContaining({
        "editor.background": background,
        "editorGutter.background": background,
        "editorSuggestWidget.background": suggestions,
        editorLineHighlightBorder: "#00000000",
      }),
    }),
  );
  expect(monaco.editor.setTheme).toHaveBeenCalledWith(name);
});
