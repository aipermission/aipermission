import { ActivityStatusBadge, formatConnectorTime } from "./sql-console-chrome";
import { tableBrowserSummary } from "./sql-console-config";
import { SQLSchemaBrowser } from "./sql-schema-browser";

const panelModes = ["browser", "requests"];

export function SQLLeftPanel({ controller, styles, theme }) {
  return (
    <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${styles.border}`}>
      <header className={`border-b px-4 py-3 ${styles.border} ${styles.subtlePanel}`}>
        <div className="flex items-center justify-between gap-3">
          <h4 className="text-sm font-semibold">
            {controller.leftPanel === "browser" ? `${controller.connector.browserLabel} browser` : "Session requests"}
          </h4>
          <div className={`inline-flex rounded-md border p-0.5 text-xs ${styles.border}`} role="tablist" aria-label="SQL navigation">
            {panelModes.map((mode) => (
              <button
                key={mode}
                type="button"
                role="tab"
                aria-selected={controller.leftPanel === mode}
                className={`rounded px-2 py-1 font-semibold transition ${controller.leftPanel === mode ? "bg-emerald-700 text-white" : `${styles.muted} ${styles.rowHover}`}`}
                onClick={() => controller.setLeftPanel(mode)}
              >
                {mode === "browser" ? "Browser" : "Requests"}
              </button>
            ))}
          </div>
        </div>
        <p className={`mt-1 text-xs ${styles.muted}`}>{panelSummary(controller)}</p>
      </header>
      <div className="min-h-0 overflow-hidden">
        {controller.leftPanel === "browser" ? (
          <SQLSchemaBrowser
            rows={controller.browserTables}
            search={controller.browserSearch}
            onSearch={controller.setBrowserSearch}
            onPrepareQuery={controller.prepareTableQuery}
            metadata={controller.metadata}
            theme={theme}
            inputClass={styles.input}
            mutedClass={styles.muted}
            hoverClass={styles.rowHover}
            namespaceLabel={controller.connector.browserLabel}
          />
        ) : (
          <SQLRequestList controller={controller} styles={styles} theme={theme} />
        )}
      </div>
    </section>
  );
}

function SQLRequestList({ controller, styles, theme }) {
  return (
    <div className={`h-full min-h-0 overflow-y-auto divide-y ${theme === "light" ? "divide-stone-200" : "divide-stone-700"}`}>
      {controller.items.map((item) => {
        const active = controller.selected && Number(controller.selected.id) === Number(item.id);
        return (
          <button
            key={item.id}
            type="button"
            aria-pressed={active}
            className={`grid w-full gap-1 px-4 py-3 text-left transition ${active ? "bg-emerald-950 text-white" : styles.rowHover}`}
            onClick={() => controller.setSelectedID(active ? null : item.id)}
          >
            <span className="flex min-w-0 items-center justify-between gap-2">
              <span className="truncate font-mono text-xs font-semibold">{item.action_name}</span>
              <ActivityStatusBadge status={item.status} />
            </span>
            <span className={`truncate text-xs ${active ? "text-emerald-100" : styles.muted}`}>
              {item.reason || formatConnectorTime(item.created_at)}
            </span>
          </button>
        );
      })}
      {controller.items.length === 0 ? <p className={`px-4 py-5 text-sm ${styles.muted}`}>No requests in this session yet.</p> : null}
    </div>
  );
}

function panelSummary(controller) {
  if (controller.leftPanel === "browser")
    return tableBrowserSummary(controller.metadata, controller.browserTables, controller.connector.browserLabel);
  return `${controller.items.length} request${controller.items.length === 1 ? "" : "s"} since ${formatConnectorTime(controller.activeSession.startedAt)}.`;
}
