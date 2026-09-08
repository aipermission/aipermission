import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Notice } from "../../../components/ui/notice";
import { metadataStatusText } from "./sql-console-config";
import { SQLEditor } from "./sql-editor";

export function SQLQueryForm({ controller, styles, theme }) {
  const running = controller.runState.state === "running";
  return (
    <form className={`grid gap-2 border-b p-3 ${styles.border} ${styles.subtlePanel}`} onSubmit={controller.runQuery}>
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-xs font-semibold">SQL</p>
          <p className={`truncate text-xs ${styles.muted}`}>{metadataStatusText(controller.metadata, controller.connector)}</p>
        </div>
        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
          <CopyButton
            value={controller.sql}
            variant="outline"
            className="h-8 px-2 text-xs"
            iconClassName="h-3.5 w-3.5"
            title="Copy SQL"
            disabled={!controller.sql.trim()}
          >
            SQL
          </CopyButton>
          {controller.recentQueries.length > 0 ? (
            <Button
              type="button"
              variant="outline"
              className="h-8 px-2 text-xs"
              onClick={() => controller.loadSQL(controller.recentQueries[0].sql)}
              title="Load the most recent query"
            >
              Last query
            </Button>
          ) : null}
          <label className="flex items-center gap-2 text-xs font-semibold">
            Max rows
            <input
              type="number"
              min="1"
              max="1000"
              className={`h-8 w-20 rounded-md border px-2 outline-none ${styles.input}`}
              value={controller.maxRows}
              onChange={(event) => controller.setMaxRows(event.target.value)}
              disabled={running}
            />
          </label>
        </div>
      </div>
      <div className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto]">
        <SQLEditor
          value={controller.sql}
          onChange={controller.setSQL}
          onSubmit={controller.runQuery}
          focusSignal={controller.editorFocusTick}
          theme={theme}
          tables={controller.metadata.tables}
          keywords={controller.connector.keywords}
          disabled={running}
        />
        <Button type="submit" className="h-full min-h-10 px-5" disabled={!controller.sql.trim() || running}>
          {running ? "Running" : "Run SQL (Ctrl+Enter)"}
        </Button>
      </div>
      {controller.runState.error ? <Notice tone="bad">{controller.runState.error}</Notice> : null}
      {controller.recentQueries.length > 0 ? <RecentQueries controller={controller} mutedClass={styles.muted} theme={theme} /> : null}
    </form>
  );
}

function RecentQueries({ controller, mutedClass, theme }) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-2">
      <span className={`shrink-0 text-[11px] font-semibold uppercase ${mutedClass}`}>Recent</span>
      {controller.recentQueries.slice(0, 5).map((query) => (
        <button
          key={`${query.id}:${query.preview}`}
          type="button"
          className={`max-w-64 truncate rounded-full border px-2.5 py-1 text-left font-mono text-[11px] transition ${theme === "light" ? "border-stone-200 bg-white text-stone-700 hover:bg-stone-100" : "border-stone-700 bg-[#1a1a1a] text-stone-200 hover:bg-stone-800"}`}
          title={query.sql}
          onClick={() => controller.loadSQL(query.sql)}
        >
          {query.preview}
        </button>
      ))}
    </div>
  );
}
