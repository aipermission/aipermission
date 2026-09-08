import { SQLLeftPanel } from "./sql-left-panel";
import { SQLRequestDetail } from "./sql-request-detail";

export function SQLConsoleWorkspace({ controller, styles, theme }) {
  return (
    <div
      className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)] gap-4 overflow-hidden p-4 ${controller.resultView ? "grid-cols-1" : "lg:grid-cols-[320px_minmax(0,1fr)]"}`}
    >
      {controller.resultView ? null : <SQLLeftPanel controller={controller} styles={styles} theme={theme} />}
      <SQLRequestDetail controller={controller} styles={styles} theme={theme} />
    </div>
  );
}
