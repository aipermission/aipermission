import { RefreshCcw, Search } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input, Select } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { resourceKey, resourceStatus, resourceSubtitle, resourceTabs, resourceTertiary, resourceTitle, resourceTone } from "./helpers";

export function KubernetesResourceBrowser({ browser, styles, theme }) {
  return (
    <section className={`grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${styles.border} ${styles.subtlePanel}`}>
      <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${styles.border}`}>
        <div><p className="text-sm font-semibold">Kubernetes resources</p><p className={`text-xs ${styles.muted}`}>{browser.filteredResources.length} shown · {browser.activeResources.length} loaded</p></div>
        <div className="flex items-center gap-2">
          {browser.latestAction ? <Badge tone={actionTone(browser.latestAction.status)}>{browser.latestAction.action_name}</Badge> : null}
          <Button type="button" variant="outline" className="h-8 w-8 px-0" title="Refresh" aria-label="Refresh resources" onClick={() => browser.refreshResource(browser.tab)} disabled={browser.state.state !== "idle"}><RefreshCcw className="h-3.5 w-3.5" /></Button>
        </div>
      </div>
      <div className={`grid grid-cols-3 gap-1 border-b p-2 ${styles.border}`} role="tablist" aria-label="Kubernetes resource type">
        {resourceTabs.map((item) => <button type="button" role="tab" aria-selected={browser.tab === item.key} key={item.key} className={`rounded-md px-2 py-1.5 text-xs font-semibold transition ${browser.tab === item.key ? "bg-emerald-600 text-white" : theme === "light" ? "text-stone-600 hover:bg-stone-100" : "text-stone-300 hover:bg-stone-800"}`} onClick={() => browser.switchTab(item.key)}>{item.label}</button>)}
      </div>
      <div className={`grid gap-2 border-b p-3 ${styles.border}`}>
        <Select value={browser.namespace} onChange={(event) => browser.changeNamespace(event.target.value)} aria-label="Kubernetes namespace"><option value="">All allowed namespaces</option>{browser.namespaces.map((item) => <option value={item.name} key={item.name}>{item.name}</option>)}</Select>
        <div className="relative"><Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${styles.muted}`} /><Input className={`pl-9 ${styles.input}`} value={browser.filter} onChange={(event) => browser.setFilter(event.target.value)} placeholder={`Filter ${browser.activeTab.label.toLowerCase()}`} aria-label={`Filter ${browser.activeTab.label.toLowerCase()}`} /></div>
      </div>
      <div className="min-h-0 overflow-auto">
        {browser.filteredResources.map((resource) => {
          const key = resourceKey(browser.tab, resource);
          return <button key={key} type="button" aria-pressed={browser.selectedKey === key} className={`grid w-full gap-1 border-b px-3 py-3 text-left text-sm transition ${styles.border} ${styles.rowHover} ${browser.selectedKey === key ? styles.activeRow : ""}`} onClick={() => browser.selectResource(resource)}><span className="flex min-w-0 items-center justify-between gap-3"><span className="truncate font-semibold" title={resourceTitle(browser.tab, resource)}>{resourceTitle(browser.tab, resource)}</span>{resourceStatus(browser.tab, resource) ? <Badge tone={resourceTone(browser.tab, resource)}>{resourceStatus(browser.tab, resource)}</Badge> : null}</span><span className={`truncate text-xs ${styles.muted}`}>{resourceSubtitle(browser.tab, resource)}</span><span className={`truncate text-xs ${styles.muted}`}>{resourceTertiary(browser.tab, resource)}</span></button>;
        })}
        {browser.filteredResources.length === 0 ? <Notice>{browser.state.state === "loading" ? "Loading Kubernetes resources..." : "No resources found for this filter."}</Notice> : null}
      </div>
    </section>
  );
}

function actionTone(status) {
  if (status === "failed") return "bad";
  if (status === "completed") return "good";
  return "warn";
}
