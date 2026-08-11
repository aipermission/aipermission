import { useEffect, useRef, useState } from "react";
import {
  cleanSQLIdentifier,
  normalizeSQLName,
  referencedTablesFromSQL,
  tableMatchesReference,
  tableReferenceKey,
} from "./sql-console-data";

export function SQLEditor({ value, onChange, onSubmit, focusSignal, theme, tables, keywords, disabled }) {
  const containerRef = useRef(null);
  const editorRef = useRef(null);
  const changeRef = useRef(null);
  const providerRef = useRef(null);
  const submitRef = useRef(onSubmit);
  const onChangeRef = useRef(onChange);
  const tablesRef = useRef(tables);
  const keywordsRef = useRef(keywords);
  const initialValueRef = useRef(value);
  const initialThemeRef = useRef(theme);
  const initialDisabledRef = useRef(disabled);
  const [monaco, setMonaco] = useState(null);

  useEffect(() => {
    submitRef.current = onSubmit;
  }, [onSubmit]);

  useEffect(() => {
    onChangeRef.current = onChange;
  }, [onChange]);

  useEffect(() => {
    tablesRef.current = tables;
  }, [tables]);

  useEffect(() => {
    keywordsRef.current = keywords;
  }, [keywords]);

  useEffect(() => {
    let canceled = false;
    loadMonaco().then((monacoInstance) => {
      if (canceled || !containerRef.current) return;
      setMonaco(monacoInstance);
      providerRef.current = monacoInstance.languages.registerCompletionItemProvider("sql", {
        triggerCharacters: [".", " ", '"'],
        provideCompletionItems(model, position) {
          return { suggestions: sqlCompletionItems(monacoInstance, tablesRef.current, keywordsRef.current, model, position) };
        },
      });
      const editor = monacoInstance.editor.create(containerRef.current, {
        value: initialValueRef.current || "",
        language: "sql",
        theme: sqlEditorTheme(monacoInstance, initialThemeRef.current),
        minimap: { enabled: false },
        automaticLayout: true,
        scrollBeyondLastLine: false,
        wordWrap: "on",
        quickSuggestions: { other: true, comments: false, strings: false },
        quickSuggestionsDelay: 40,
        suggestOnTriggerCharacters: true,
        wordBasedSuggestions: "off",
        tabCompletion: "on",
        acceptSuggestionOnEnter: "on",
        acceptSuggestionOnCommitCharacter: true,
        fixedOverflowWidgets: true,
        suggest: {
          showWords: false,
          snippetsPreventQuickSuggestions: false,
          selectionMode: "always",
        },
        fontSize: 12,
        lineHeight: 18,
        lineNumbers: "on",
        glyphMargin: false,
        folding: false,
        lineDecorationsWidth: 8,
        lineNumbersMinChars: 3,
        overviewRulerLanes: 0,
        hideCursorInOverviewRuler: true,
        renderLineHighlight: "none",
        tabSize: 2,
        readOnly: initialDisabledRef.current,
        domReadOnly: initialDisabledRef.current,
        padding: { top: 8, bottom: 8 },
      });
      editorRef.current = editor;
      editor.addCommand(monacoInstance.KeyMod.CtrlCmd | monacoInstance.KeyCode.Enter, () => submitRef.current?.());
      changeRef.current = editor.onDidChangeModelContent(() => {
        onChangeRef.current(editor.getValue());
      });
    });
    return () => {
      canceled = true;
      providerRef.current?.dispose();
      changeRef.current?.dispose();
      editorRef.current?.dispose();
      providerRef.current = null;
      changeRef.current = null;
      editorRef.current = null;
    };
  }, []);

  useEffect(() => {
    const editor = editorRef.current;
    if (!editor || editor.getValue() === value) return;
    editor.setValue(value || "");
  }, [value]);

  useEffect(() => {
    if (!monaco) return;
    monaco.editor.setTheme(sqlEditorTheme(monaco, theme));
  }, [monaco, theme]);

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly: disabled, domReadOnly: disabled });
  }, [disabled]);

  useEffect(() => {
    if (!focusSignal) return;
    window.setTimeout(() => editorRef.current?.focus(), 0);
  }, [focusSignal]);

  return (
    <div
      ref={containerRef}
      className={`min-h-28 overflow-visible rounded-md border ${theme === "light" ? "border-stone-300 bg-stone-50" : "border-stone-700 bg-[#252526]"}`}
    />
  );
}

let monacoPromise = null;

function loadMonaco() {
  if (!monacoPromise) {
    monacoPromise = import("monaco-editor/esm/vs/editor/editor.worker?worker").then((workerModule) => {
      if (typeof window !== "undefined") {
        window.MonacoEnvironment = {
          getWorker() {
            return new workerModule.default();
          },
        };
      }
      return Promise.all([
        import("monaco-editor/esm/vs/basic-languages/sql/sql.contribution"),
        import("monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController.js"),
        import("monaco-editor/esm/vs/editor/editor.api"),
      ]).then(([, , monaco]) => monaco);
    });
  }
  return monacoPromise;
}

function sqlEditorTheme(monaco, theme) {
  const dark = theme !== "light";
  const name = dark ? "aipermission-sql-dark" : "aipermission-sql-light";
  monaco.editor.defineTheme(name, {
    base: dark ? "vs-dark" : "vs",
    inherit: true,
    rules: [],
    colors: {
      "editor.background": dark ? "#252526" : "#fafaf9",
      "editorGutter.background": dark ? "#252526" : "#fafaf9",
      "editorLineNumber.foreground": dark ? "#78716c" : "#a8a29e",
      "editorCursor.foreground": dark ? "#e7e5e4" : "#1c1917",
      "editor.selectionBackground": dark ? "#064e3b" : "#bbf7d0",
      editorLineHighlightBorder: "#00000000",
      editorLineHighlightBackground: "#00000000",
      "editorIndentGuide.background1": "#00000000",
      "editorIndentGuide.activeBackground1": "#00000000",
      "editorSuggestWidget.background": dark ? "#252526" : "#ffffff",
      "editorSuggestWidget.border": dark ? "#44403c" : "#d6d3d1",
      "editorSuggestWidget.foreground": dark ? "#e7e5e4" : "#292524",
      "editorSuggestWidget.selectedBackground": dark ? "#064e3b" : "#dcfce7",
      "editorSuggestWidget.highlightForeground": dark ? "#6ee7b7" : "#047857",
    },
  });
  return name;
}

function sqlCompletionItems(monaco, tables, keywords, model, position) {
  const word = model.getWordUntilPosition(position);
  const range = {
    startLineNumber: position.lineNumber,
    endLineNumber: position.lineNumber,
    startColumn: word.startColumn,
    endColumn: word.endColumn,
  };
  const suggestions = keywords.map((keyword) => ({
    label: keyword.toUpperCase(),
    kind: monaco.languages.CompletionItemKind.Keyword,
    insertText: keyword,
    sortText: `2_${keyword}`,
    range,
  }));
  const seenSchemas = new Set();
  const seenTables = new Set();
  const tableReferences = referencedTablesFromSQL(model.getValue());
  const dotReference = dotReferenceBeforePosition(model, position);
  const columnReferences = dotReference ? matchingReferencesForQualifier(dotReference, tableReferences, tables) : tableReferences;
  const inTableContext = isTableCompletionContext(model, position);
  const seenColumns = new Set();
  for (const item of tables || []) {
    if (item.schema && !seenSchemas.has(item.schema)) {
      seenSchemas.add(item.schema);
      suggestions.push({
        label: item.schema,
        kind: monaco.languages.CompletionItemKind.Module,
        insertText: quoteSQLIdentifier(item.schema),
        detail: "schema",
        sortText: `1_schema_${item.schema}`,
        range,
      });
    }
    const tableKey = `${item.schema}.${item.table}`;
    if (!seenTables.has(tableKey)) {
      seenTables.add(tableKey);
      suggestions.push({
        label: item.table,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: quoteSQLIdentifier(item.table),
        detail: item.schema,
        documentation: item.type || "table",
        sortText: `0_table_${item.table}`,
        range,
      });
      suggestions.push({
        label: tableKey,
        kind: monaco.languages.CompletionItemKind.Class,
        insertText: `${quoteSQLIdentifier(item.schema)}.${quoteSQLIdentifier(item.table)}`,
        detail: item.type || "table",
        sortText: `0_full_${tableKey}`,
        range,
      });
    }
    if (!inTableContext && item.column && columnReferences.some((reference) => tableMatchesReference(item, reference))) {
      const columnKey = `${tableKey}.${item.column}`;
      if (!seenColumns.has(columnKey)) {
        seenColumns.add(columnKey);
        suggestions.push({
          label: item.column,
          kind: monaco.languages.CompletionItemKind.Field,
          insertText: quoteSQLIdentifier(item.column),
          detail: `${tableKey}${item.dataType ? ` / ${item.dataType}` : ""}`,
          sortText: `0_column_${item.column}_${columnKey}`,
          range,
        });
      }
    }
  }
  return suggestions;
}

function matchingReferencesForQualifier(qualifier, references, metadataRows) {
  const normalized = normalizeSQLName(qualifier);
  const matches = references.filter(
    (reference) => normalizeSQLName(reference.alias) === normalized || normalizeSQLName(reference.table) === normalized,
  );
  if (matches.length > 0) return matches;
  const metadataMatches = [];
  const seen = new Set();
  for (const item of metadataRows || []) {
    if (normalizeSQLName(item.table) !== normalized) continue;
    const reference = { schema: item.schema || "", table: item.table || "", alias: "" };
    const key = tableReferenceKey(reference);
    if (seen.has(key)) continue;
    seen.add(key);
    metadataMatches.push(reference);
  }
  return metadataMatches;
}

function dotReferenceBeforePosition(model, position) {
  const prefix = model.getLineContent(position.lineNumber).slice(0, position.column - 1);
  const match = prefix.match(/((?:"[^"]+"|`[^`]+`|[a-zA-Z_][\w$]*))\.\s*(?:"[^"]*"|`[^`]*`|[a-zA-Z_][\w$]*)?$/);
  return match ? cleanSQLIdentifier(match[1]) : "";
}

function isTableCompletionContext(model, position) {
  const prefix = model
    .getLineContent(position.lineNumber)
    .slice(0, position.column - 1)
    .toLowerCase();
  return /\b(from|join)\s+(?:"[^"]*"|`[^`]*`|[a-z_][\w$]*)?$/i.test(prefix);
}

function quoteSQLIdentifier(value) {
  if (/^[a-z_][a-z0-9_]*$/.test(value)) return value;
  return `"${String(value).replaceAll('"', '""')}"`;
}
