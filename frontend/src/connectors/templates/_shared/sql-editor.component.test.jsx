import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { SQLEditor } from "./sql-editor";
import { applySQLEditorTheme, loadSQLMonaco } from "./sql-editor-runtime";

vi.mock("./sql-editor-runtime", () => ({
  applySQLEditorTheme: vi.fn(() => "test-theme"),
  loadSQLMonaco: vi.fn(),
}));

let command;
let completionProvider;
let changeListener;
let editor;
let monaco;

beforeEach(() => {
  command = null;
  completionProvider = null;
  changeListener = null;
  editor = {
    addCommand: vi.fn((_key, callback) => (command = callback)),
    dispose: vi.fn(),
    focus: vi.fn(),
    getValue: vi.fn(() => "SELECT 2"),
    onDidChangeModelContent: vi.fn((callback) => {
      changeListener = callback;
      return { dispose: vi.fn() };
    }),
    setValue: vi.fn(),
    updateOptions: vi.fn(),
  };
  monaco = {
    KeyMod: { CtrlCmd: 1 },
    KeyCode: { Enter: 2 },
    languages: {
      CompletionItemKind: { Keyword: 1, Module: 2, Class: 3, Field: 4 },
      registerCompletionItemProvider: vi.fn((_language, provider) => {
        completionProvider = provider;
        return { dispose: vi.fn() };
      }),
    },
    editor: { create: vi.fn(() => editor) },
  };
  loadSQLMonaco.mockReset().mockResolvedValue(monaco);
  applySQLEditorTheme.mockClear();
});

it("wires SQL completion, Ctrl/Cmd+Enter, changes, and editor options", async () => {
  const onChange = vi.fn();
  const onSubmit = vi.fn();
  render(
    <SQLEditor
      value="SELECT 1"
      onChange={onChange}
      onSubmit={onSubmit}
      focusSignal={0}
      theme="dark"
      tables={[{ schema: "public", table: "users", column: "id" }]}
      keywords={["select"]}
      disabled={false}
    />,
  );

  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());
  expect(monaco.editor.create.mock.calls[0][1]).toMatchObject({
    value: "SELECT 1",
    language: "sql",
    theme: "test-theme",
    acceptSuggestionOnEnter: "on",
    fixedOverflowWidgets: true,
  });
  expect(editor.addCommand).toHaveBeenCalledWith(3, expect.any(Function));
  command();
  expect(onSubmit).toHaveBeenCalledOnce();
  changeListener();
  expect(onChange).toHaveBeenCalledWith("SELECT 2");

  const model = {
    getValue: () => "SELECT * FROM public.users u WHERE u.",
    getLineContent: () => "SELECT * FROM public.users u WHERE u.",
    getWordUntilPosition: () => ({ startColumn: 38, endColumn: 38 }),
  };
  const result = completionProvider.provideCompletionItems(model, { lineNumber: 1, column: 39 });
  expect(result.suggestions).toEqual(expect.arrayContaining([expect.objectContaining({ label: "id", kind: 4 })]));
});

it("shows a bounded error when the editor chunk cannot load", async () => {
  loadSQLMonaco.mockRejectedValueOnce(new Error("editor chunk unavailable"));

  render(
    <SQLEditor value="" onChange={vi.fn()} onSubmit={vi.fn()} focusSignal={0} theme="dark" tables={[]} keywords={[]} disabled={false} />,
  );

  expect(await screen.findByRole("alert")).toHaveTextContent("editor chunk unavailable");
});

it("uses the latest controlled text and disabled state after a deferred editor load", async () => {
  let resolveMonaco;
  loadSQLMonaco.mockReturnValueOnce(new Promise((resolve) => (resolveMonaco = resolve)));
  const props = { onChange: vi.fn(), onSubmit: vi.fn(), focusSignal: 0, tables: [], keywords: [] };
  const { rerender } = render(<SQLEditor {...props} value="SELECT old" theme="dark" disabled={false} />);
  rerender(<SQLEditor {...props} value="SELECT new" theme="light" disabled={true} />);

  resolveMonaco(monaco);
  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());
  expect(monaco.editor.create.mock.calls[0][1]).toMatchObject({
    value: "SELECT new",
    theme: "test-theme",
    readOnly: true,
    domReadOnly: true,
  });
  expect(applySQLEditorTheme).toHaveBeenCalledWith(monaco, "light");
});

it("updates the loaded editor when controlled SQL and focus change", async () => {
  const props = { onChange: vi.fn(), onSubmit: vi.fn(), tables: [], keywords: [], theme: "dark", disabled: false };
  const { rerender } = render(<SQLEditor {...props} value="SELECT old" focusSignal={0} />);
  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());

  rerender(<SQLEditor {...props} value="SELECT new" focusSignal={1} />);
  expect(editor.setValue).toHaveBeenCalledWith("SELECT new");
  await waitFor(() => expect(editor.focus).toHaveBeenCalled());
});
