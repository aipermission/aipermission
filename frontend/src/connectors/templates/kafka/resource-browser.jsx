import { Database, RefreshCcw, Search, Users } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";

export function KafkaResourceBrowser({ browser, styles }) {
  return (
    <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${styles.border} ${styles.subtlePanel}`}>
      <div className={`grid grid-cols-2 gap-1 border-b p-2 ${styles.border}`} role="tablist" aria-label={`${browser.product} browser view`}>
        <ViewButton selected={browser.view === "topics"} onClick={() => void browser.changeView("topics")} icon={Database}>Topics</ViewButton>
        <ViewButton selected={browser.view === "groups"} onClick={() => void browser.changeView("groups")} icon={Users}>Groups</ViewButton>
      </div>
      <div className={`grid grid-cols-[minmax(0,1fr)_auto] gap-2 border-b p-3 ${styles.border}`}>
        <div className="relative">
          <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${styles.muted}`} />
          <Input className={`pl-9 ${styles.input}`} value={browser.query} onChange={(event) => browser.setQuery(event.target.value)} placeholder={`Filter ${browser.view}`} aria-label={`Filter ${browser.view}`} />
        </div>
        <Button type="button" variant="outline" className="h-9 w-9 px-0" title={`Refresh ${browser.view}`} aria-label={`Refresh ${browser.view}`} onClick={() => void browser.refreshList()} disabled={browser.state.state !== "idle"}>
          <RefreshCcw className="h-4 w-4" />
        </Button>
      </div>
      <div className="min-h-0 overflow-auto p-2">
        {browser.filteredItems.map((item) => (
          <button key={item.name} type="button" className={`mb-1 grid w-full gap-1 rounded-md border px-3 py-2 text-left transition ${browser.selectedName === item.name ? styles.activeRow : `${styles.border} ${styles.rowHover}`}`} onClick={() => void browser.selectItem(item)} aria-pressed={browser.selectedName === item.name}>
            <span className="truncate font-mono text-xs font-semibold" title={item.name}>{item.name}</span>
            <span className={`truncate text-xs ${browser.selectedName === item.name ? "" : styles.muted}`}>{itemSummary(browser.view, item)}</span>
          </button>
        ))}
        {browser.filteredItems.length === 0 ? <Notice>{browser.state.state === "loading" ? `Loading ${browser.view}...` : `No ${browser.view} found.`}</Notice> : null}
      </div>
    </section>
  );
}

function ViewButton({ selected, onClick, icon: Icon, children }) {
  return <Button type="button" role="tab" aria-selected={selected} variant={selected ? "default" : "outline"} className="h-9" onClick={onClick}><Icon className="h-4 w-4" />{children}</Button>;
}

function itemSummary(view, item) {
  return view === "topics" ? `${item.partition_count || 0} partitions · replication ${item.replication_factor || 0}` : `${item.state || "unknown"} · ${item.protocol_type || "unknown"}`;
}
