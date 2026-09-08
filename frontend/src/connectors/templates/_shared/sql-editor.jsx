import { useEffect, useRef, useState } from "react";
import { sqlCompletionItems } from "./sql-editor-completions";
import { applySQLEditorTheme, loadSQLMonaco } from "./sql-editor-runtime";

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
  const [loadError, setLoadError] = useState("");

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
    loadSQLMonaco()
      .then((instance) => {
        if (canceled || !containerRef.current) return;
        setLoadError("");
        setMonaco(instance);
        providerRef.current = instance.languages.registerCompletionItemProvider("sql", {
          triggerCharacters: [".", " ", '"'],
          provideCompletionItems(model, position) {
            return { suggestions: sqlCompletionItems(instance, tablesRef.current, keywordsRef.current, model, position) };
          },
        });
        const editor = instance.editor.create(
          containerRef.current,
          editorOptions(instance, initialValueRef.current, initialThemeRef.current, initialDisabledRef.current),
        );
        editorRef.current = editor;
        editor.addCommand(instance.KeyMod.CtrlCmd | instance.KeyCode.Enter, () => submitRef.current?.());
        changeRef.current = editor.onDidChangeModelContent(() => onChangeRef.current(editor.getValue()));
      })
      .catch((error) => {
        if (!canceled) setLoadError(error?.message || "SQL editor could not be loaded.");
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
    if (editor && editor.getValue() !== value) editor.setValue(value || "");
  }, [value]);

  useEffect(() => {
    if (monaco) applySQLEditorTheme(monaco, theme);
  }, [monaco, theme]);

  useEffect(() => {
    editorRef.current?.updateOptions({ readOnly: disabled, domReadOnly: disabled });
  }, [disabled]);
  useEffect(() => {
    if (!focusSignal) return;
    const timer = window.setTimeout(() => editorRef.current?.focus(), 0);
    return () => window.clearTimeout(timer);
  }, [focusSignal]);

  return (
    <div
      ref={containerRef}
      aria-label="SQL editor"
      className={`min-h-28 overflow-visible rounded-md border ${theme === "light" ? "border-stone-300 bg-stone-50" : "border-stone-700 bg-[#252526]"}`}
    >
      {loadError ? (
        <p role="alert" className="p-3 text-sm text-red-400">
          {loadError}
        </p>
      ) : null}
    </div>
  );
}

function editorOptions(monaco, value, theme, disabled) {
  return {
    value: value || "",
    language: "sql",
    theme: applySQLEditorTheme(monaco, theme),
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
    suggest: { showWords: false, snippetsPreventQuickSuggestions: false, selectionMode: "always" },
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
    readOnly: disabled,
    domReadOnly: disabled,
    padding: { top: 8, bottom: 8 },
  };
}
