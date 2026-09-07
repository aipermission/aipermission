let monacoPromise = null;

export function loadSQLMonaco() {
  if (!monacoPromise) {
    monacoPromise = import("monaco-editor/esm/vs/editor/editor.worker?worker")
      .then((workerModule) => {
        if (typeof window !== "undefined") {
          window.MonacoEnvironment = { getWorker: () => new workerModule.default() };
        }
        return Promise.all([
          import("monaco-editor/esm/vs/basic-languages/sql/sql.contribution"),
          import("monaco-editor/esm/vs/editor/contrib/suggest/browser/suggestController.js"),
          import("monaco-editor/esm/vs/editor/editor.api"),
        ]).then(([, , monaco]) => monaco);
      })
      .catch((error) => {
        monacoPromise = null;
        throw error;
      });
  }
  return monacoPromise;
}

export function applySQLEditorTheme(monaco, theme) {
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
  monaco.editor.setTheme(name);
  return name;
}
