import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Notice } from "../../../components/ui/notice";
import { ActivityStatusBadge, formatConnectorTime, ResultViewToggle } from "./sql-console-chrome";
import { ActivityBlock, SQLOutputBlock } from "./sql-result-output";

export function SQLRequestDetail({ controller, styles, theme }) {
  const selected = controller.selected;
  if (!selected)
    return (
      <section className={`grid h-full min-h-0 place-items-center rounded-lg border p-6 text-sm ${styles.border} ${styles.muted}`}>
        Select a session request to inspect input and output. Completed requests remain available in History.
      </section>
    );
  return (
    <section className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${styles.border}`}>
      <header className={`border-b px-4 py-3 ${styles.border} ${styles.subtlePanel}`}>
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="min-w-0">
            <h4 className="truncate text-sm font-semibold">Request #{selected.id}</h4>
            <p className={`mt-1 truncate text-xs ${styles.muted}`}>
              {selected.action_name} / {formatConnectorTime(selected.created_at)}
            </p>
          </div>
          <ActivityStatusBadge status={selected.status} />
        </div>
        <div className="mt-2 flex flex-wrap items-center justify-between gap-3">
          {selected.reason ? <p className={`min-w-0 flex-1 truncate text-xs ${styles.muted}`}>Reason: {selected.reason}</p> : <span />}
          <div className="flex shrink-0 flex-wrap items-center gap-2">
            {controller.selectedSQL ? (
              <>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  onClick={() => controller.loadSQL(controller.selectedSQL)}
                >
                  Load SQL
                </Button>
                <CopyButton
                  value={controller.selectedSQL}
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  iconClassName="h-3.5 w-3.5"
                  title="Copy request SQL"
                >
                  SQL
                </CopyButton>
              </>
            ) : null}
            <ResultViewToggle checked={controller.resultView} onChange={controller.setResultView} theme={theme} />
          </div>
        </div>
        {selected.error ? <Notice tone="bad">{selected.error}</Notice> : null}
      </header>
      <div className={`h-full min-h-0 overflow-hidden p-3 ${controller.resultView ? "" : "grid gap-3 xl:grid-cols-2"}`}>
        {controller.resultView ? (
          <SQLOutputBlock
            title="Rows"
            value={selected.output ?? selected.display_text ?? {}}
            theme={theme}
            filenamePrefix={controller.connector.filenamePrefix}
          />
        ) : (
          <>
            <ActivityBlock title="Input" value={selected.input || {}} />
            <ActivityBlock title="Output" value={selected.output ?? selected.display_text ?? {}} />
          </>
        )}
      </div>
    </section>
  );
}
