import { Plus, RefreshCcw, Search, Trash2 } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Checkbox, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function RedisKeyBrowser({ browser, styles }) {
  const allSelected = browser.selectedKeys.length === browser.keys.length && browser.keys.length > 0;
  return (
    <section
      className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${styles.border} ${styles.subtlePanel}`}
    >
      <header className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${styles.border}`}>
        <div>
          <p className="text-sm font-semibold">Keys</p>
          <p className={`text-xs ${styles.muted}`}>{browser.keys.length} loaded</p>
        </div>
        <div className="flex flex-wrap items-center justify-end gap-2">
          {browser.latestAction ? <Badge tone={actionTone(browser.latestAction.status)}>{browser.latestAction.action_name}</Badge> : null}
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            title="Refresh keys"
            aria-label="Refresh keys"
            onClick={() => browser.scanKeys({ reset: true })}
            disabled={browser.state.state !== "idle"}
          >
            <RefreshCcw className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={browser.startNewKey}>
            <Plus className="h-3.5 w-3.5" />
            New
          </Button>
          <Button
            type="button"
            variant="outline"
            className="h-8 px-2 text-xs"
            onClick={() => browser.setSelectedKeys(allSelected ? [] : browser.keys)}
          >
            {allSelected ? "None" : "All"}
          </Button>
        </div>
      </header>
      <form
        className={`grid gap-2 border-b p-3 ${styles.border}`}
        onSubmit={(event) => {
          event.preventDefault();
          void browser.scanKeys({ reset: true });
        }}
      >
        <div className="relative">
          <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${styles.muted}`} />
          <Input
            className={`pl-9 ${styles.input}`}
            value={browser.pattern}
            onChange={(event) => browser.setPattern(event.target.value)}
            placeholder="SCAN pattern, e.g. user:*"
            aria-label="Redis key scan pattern"
          />
        </div>
        <Button type="submit" variant="outline" className="h-9" disabled={browser.state.state !== "idle"}>
          {browser.state.state === "scanning" ? "Scanning" : "Scan keys"}
        </Button>
      </form>
      <div className="min-h-0 overflow-auto p-2">
        {browser.keys.map((key) => (
          <RedisKeyRow key={key} value={key} browser={browser} styles={styles} />
        ))}
        {browser.keys.length === 0 ? (
          <Notice>
            {browser.state.state === "scanning" ? `Scanning ${browser.product} keys...` : "No keys loaded. Scan to browse this database."}
          </Notice>
        ) : null}
      </div>
      <footer className={`flex items-center justify-between gap-2 border-t p-3 ${styles.border}`}>
        <Button
          type="button"
          variant="outline"
          className="h-8 px-3 text-xs"
          disabled={browser.cursor === "0" || browser.state.state !== "idle"}
          onClick={() => browser.scanKeys({ reset: false })}
        >
          More
        </Button>
        <Button
          type="button"
          variant="outline"
          className="h-8 px-3 text-xs text-red-600"
          disabled={(browser.selectedCount === 0 && !browser.activeKey) || browser.state.state !== "idle"}
          onClick={browser.deleteSelected}
        >
          <Trash2 className="h-3.5 w-3.5" />
          Delete {browser.selectedCount || (browser.activeKey ? 1 : "")}
        </Button>
      </footer>
    </section>
  );
}

function RedisKeyRow({ value, browser, styles }) {
  return (
    <button
      type="button"
      aria-pressed={browser.activeKey === value}
      className={`mb-1 grid w-full grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md border px-2 py-2 text-left text-sm transition ${browser.activeKey === value ? styles.activeRow : `${styles.border} ${styles.rowHover}`}`}
      onClick={() => browser.loadKey(value)}
    >
      <Checkbox
        checked={browser.selectedKeys.includes(value)}
        onClick={(event) => event.stopPropagation()}
        onChange={() => browser.toggleSelection(value)}
        aria-label={`Select ${value}`}
      />
      <span className="truncate font-mono text-xs" title={value}>
        {value}
      </span>
    </button>
  );
}

function actionTone(status) {
  if (status === "failed") return "bad";
  if (status === "completed") return "good";
  return "warn";
}
