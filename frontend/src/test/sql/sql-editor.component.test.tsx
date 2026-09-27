import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { editor as MonacoEditor } from "monaco-editor";
import { SQLEditor } from "../../connectors/templates/_shared/sql-editor";
import type { SQLEditorProps } from "../../connectors/templates/_shared/sql-editor";
import { tableMatchesReference, tableReferenceKey } from "../../connectors/templates/_shared/sql-console-data";
import type { sqlCompletionItems } from "../../connectors/templates/_shared/sql-editor-completions";

type CompletionProvider = {
  provideCompletionItems: (
    _model: Parameters<typeof sqlCompletionItems>[3],
    _position: Parameters<typeof sqlCompletionItems>[4],
  ) => { suggestions: ReturnType<typeof sqlCompletionItems> };
};
type TestMonaco = ReturnType<typeof createMonaco>;

const { applySQLEditorTheme, loadSQLMonaco } = vi.hoisted(() => ({
  applySQLEditorTheme: vi.fn((_monaco: TestMonaco, _theme: string) => "test-theme"),
  loadSQLMonaco: vi.fn<() => Promise<TestMonaco>>(),
}));
vi.mock("../../connectors/templates/_shared/sql-editor-runtime", () => ({ applySQLEditorTheme, loadSQLMonaco }));

let command: (() => unknown) | null;
let completionProvider: CompletionProvider | null;
let changeListener: (() => void) | null;
let editor: ReturnType<typeof createEditor>;
let monaco: TestMonaco;

function createEditor() {
  return {
    addCommand: vi.fn((_key: number, callback: () => unknown) => (command = callback)),
    dispose: vi.fn(),
    focus: vi.fn(),
    getValue: vi.fn(() => "SELECT 2"),
    onDidChangeModelContent: vi.fn((callback: () => void) => {
      changeListener = callback;
      return { dispose: vi.fn() };
    }),
    setValue: vi.fn(),
    updateOptions: vi.fn(),
  };
}

function createMonaco(instance: ReturnType<typeof createEditor>) {
  return {
    KeyMod: { CtrlCmd: 1 },
    KeyCode: { Enter: 2 },
    languages: {
      CompletionItemKind: { Keyword: 1, Module: 2, Class: 3, Field: 4 },
      registerCompletionItemProvider: vi.fn((_language: string, provider: CompletionProvider) => {
        completionProvider = provider;
        return { dispose: vi.fn() };
      }),
    },
    editor: { create: vi.fn((_container: HTMLElement, _options: MonacoEditor.IStandaloneEditorConstructionOptions) => instance) },
  };
}

function registeredProvider(): CompletionProvider {
  if (!completionProvider) throw new Error("Completion provider was not registered");
  return completionProvider;
}

beforeEach(() => {
  command = null;
  completionProvider = null;
  changeListener = null;
  editor = createEditor();
  monaco = createMonaco(editor);
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
  if (!command || !changeListener) throw new Error("Editor callbacks were not registered");
  command();
  expect(onSubmit).toHaveBeenCalledOnce();
  changeListener();
  expect(onChange).toHaveBeenCalledWith("SELECT 2");

  const model = {
    getValue: () => "SELECT * FROM public.users u WHERE u.",
    getLineContent: () => "SELECT * FROM public.users u WHERE u.",
    getWordUntilPosition: () => ({ startColumn: 38, endColumn: 38 }),
  };
  const result = registeredProvider().provideCompletionItems(model, { lineNumber: 1, column: 39 });
  expect(result.suggestions).toEqual(expect.arrayContaining([expect.objectContaining({ label: "id", kind: 4 })]));
});

it("keeps completion columns bound to the exact quoted table identity", async () => {
  render(
    <SQLEditor
      value={'SELECT * FROM public."Users" u WHERE u.'}
      onChange={vi.fn()}
      onSubmit={vi.fn()}
      focusSignal={0}
      theme="dark"
      tables={[
        { schema: "public", table: "Users", column: "admin_id" },
        { schema: "public", table: "users", column: "public_id" },
      ]}
      keywords={[]}
      disabled={false}
    />,
  );

  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());
  const sql = 'SELECT * FROM public."Users" u WHERE u.';
  const model = {
    getValue: () => sql,
    getLineContent: () => sql,
    getWordUntilPosition: () => ({ startColumn: sql.length + 1, endColumn: sql.length + 1 }),
  };
  const result = registeredProvider().provideCompletionItems(model, { lineNumber: 1, column: sql.length + 1 });

  expect(result.suggestions).toEqual(expect.arrayContaining([expect.objectContaining({ label: "admin_id", kind: 4 })]));
  expect(result.suggestions).not.toEqual(expect.arrayContaining([expect.objectContaining({ label: "public_id", kind: 4 })]));
  expect(tableReferenceKey({ schema: "Analytics", table: "Users", schemaQuoted: false, tableQuoted: false }, "exact")).toBe(
    tableReferenceKey({ schema: "Analytics", table: "Users", schemaQuoted: true, tableQuoted: true }, "exact"),
  );
  expect(tableMatchesReference({ schema: "Analytics", table: "Users" }, { schema: "Analytics", table: "users" }, "exact")).toBe(false);
  expect(tableMatchesReference({ schema: "public", table: "users" }, { schema: "PUBLIC", table: "Users" })).toBe(true);
});

it("preserves unquoted ClickHouse identifier case when matching metadata", async () => {
  render(
    <SQLEditor
      value="SELECT * FROM Analytics.Users u WHERE u."
      onChange={vi.fn()}
      onSubmit={vi.fn()}
      focusSignal={0}
      theme="dark"
      tables={[
        { schema: "Analytics", table: "Users", column: "admin_id" },
        { schema: "Analytics", table: "users", column: "public_id" },
      ]}
      keywords={[]}
      identifierPolicy="exact"
      disabled={false}
    />,
  );

  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());
  const sql = "SELECT * FROM Analytics.Users u WHERE u.";
  const model = {
    getValue: () => sql,
    getLineContent: () => sql,
    getWordUntilPosition: () => ({ startColumn: sql.length + 1, endColumn: sql.length + 1 }),
  };
  const result = registeredProvider().provideCompletionItems(model, { lineNumber: 1, column: sql.length + 1 });

  expect(result.suggestions).toEqual(expect.arrayContaining([expect.objectContaining({ label: "admin_id", kind: 4 })]));
  expect(result.suggestions).not.toEqual(expect.arrayContaining([expect.objectContaining({ label: "public_id", kind: 4 })]));
});

it("shows a bounded error when the editor chunk cannot load", async () => {
  loadSQLMonaco.mockRejectedValueOnce(new Error("editor chunk unavailable"));

  render(
    <SQLEditor value="" onChange={vi.fn()} onSubmit={vi.fn()} focusSignal={0} theme="dark" tables={[]} keywords={[]} disabled={false} />,
  );

  expect(await screen.findByRole("alert")).toHaveTextContent("editor chunk unavailable");
});

it("uses the latest controlled text and disabled state after a deferred editor load", async () => {
  let resolveMonaco: ((_monaco: TestMonaco) => void) | undefined;
  loadSQLMonaco.mockReturnValueOnce(new Promise<TestMonaco>((resolve) => (resolveMonaco = resolve)));
  const props = { onChange: vi.fn(), onSubmit: vi.fn(), focusSignal: 0, tables: [], keywords: [] };
  const { rerender } = render(<SQLEditor {...props} value="SELECT old" theme="dark" disabled={false} />);
  rerender(<SQLEditor {...props} value="SELECT new" theme="light" disabled={true} />);

  if (!resolveMonaco) throw new Error("Editor load promise was not created");
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
  const props = { onChange: vi.fn(), onSubmit: vi.fn(), tables: [], keywords: [], theme: "dark", disabled: false } satisfies Omit<
    SQLEditorProps,
    "value" | "focusSignal"
  >;
  const { rerender } = render(<SQLEditor {...props} value="SELECT old" focusSignal={0} />);
  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());

  rerender(<SQLEditor {...props} value="SELECT new" focusSignal={1} />);
  expect(editor.setValue).toHaveBeenCalledWith("SELECT new");
  await waitFor(() => expect(editor.focus).toHaveBeenCalled());
});

it("disposes its completion provider, change listener, and editor on unmount", async () => {
  const { unmount } = render(
    <SQLEditor
      value="SELECT 1"
      onChange={vi.fn()}
      onSubmit={vi.fn()}
      focusSignal={0}
      theme="dark"
      tables={[]}
      keywords={[]}
      disabled={false}
    />,
  );
  await waitFor(() => expect(monaco.editor.create).toHaveBeenCalled());
  const provider = monaco.languages.registerCompletionItemProvider.mock.results[0].value;
  const listener = editor.onDidChangeModelContent.mock.results[0].value;

  unmount();

  expect(provider?.dispose).toHaveBeenCalledOnce();
  expect(listener?.dispose).toHaveBeenCalledOnce();
  expect(editor.dispose).toHaveBeenCalledOnce();
});

it("does not create an editor when a deferred runtime arrives after unmount", async () => {
  let resolveMonaco: ((_monaco: TestMonaco) => void) | undefined;
  loadSQLMonaco.mockReturnValueOnce(new Promise<TestMonaco>((resolve) => (resolveMonaco = resolve)));
  const { unmount } = render(
    <SQLEditor value="SELECT 1" onChange={vi.fn()} focusSignal={0} theme="dark" tables={[]} keywords={[]} disabled={false} />,
  );
  unmount();
  if (!resolveMonaco) throw new Error("Editor load promise was not created");
  const resolve = resolveMonaco;
  await act(async () => resolve(monaco));

  expect(monaco.editor.create).not.toHaveBeenCalled();
  expect(monaco.languages.registerCompletionItemProvider).not.toHaveBeenCalled();
});
