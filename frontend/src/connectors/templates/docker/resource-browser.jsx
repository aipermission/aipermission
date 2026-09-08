import { RefreshCcw } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import {
  resourceKey,
  resourceLabel,
  resourcePrimary,
  resourceSecondary,
  resourceStatus,
  resourceTabLabel,
  resourceTertiary,
  resourceTone,
} from "./helpers";

const resourceKinds = ["containers", "images", "networks", "volumes"];

export function DockerResourceBrowser({
  resourceView,
  items,
  visibleCount,
  selectedContainer,
  selectedResourceID,
  filter,
  state,
  latestAction,
  theme,
  classes,
  onRefresh,
  onSwitchView,
  onFilter,
  onSelect,
}) {
  return (
    <section
      className={`grid h-full min-h-0 grid-rows-[auto_auto_auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${classes.border} ${classes.subtlePanel}`}
    >
      <div className={`border-b p-3 ${classes.border}`}>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <p className="text-sm font-semibold">{resourceLabel(resourceView)}</p>
            <p className={`text-xs ${classes.muted}`}>{visibleCount} visible in this profile scope</p>
          </div>
          <div className="flex items-center gap-2">
            {latestAction ? (
              <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
                {latestAction.action_name}
              </Badge>
            ) : null}
            <Button
              type="button"
              variant="outline"
              className="h-8 w-8 px-0"
              title={`Refresh ${resourceLabel(resourceView).toLowerCase()}`}
              onClick={onRefresh}
              disabled={state.state !== "idle"}
            >
              <RefreshCcw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      </div>
      <div className={`grid grid-cols-4 gap-1 border-b p-2 ${classes.border}`}>
        {resourceKinds.map((kind) => (
          <button
            type="button"
            key={kind}
            className={`rounded-md px-2 py-1.5 text-xs font-semibold ${
              resourceView === kind
                ? "bg-emerald-600 text-white"
                : theme === "light"
                  ? "text-stone-600 hover:bg-stone-100"
                  : "text-stone-300 hover:bg-stone-800"
            }`}
            onClick={() => onSwitchView(kind)}
            aria-label={resourceLabel(kind)}
            aria-pressed={resourceView === kind}
          >
            {resourceTabLabel(kind)}
          </button>
        ))}
      </div>
      <div className={`border-b p-3 ${classes.border}`}>
        <Input
          className={classes.input}
          value={filter}
          onChange={(event) => onFilter(event.target.value)}
          placeholder={`Search ${resourceLabel(resourceView).toLowerCase()}`}
        />
      </div>
      <div className="min-h-0 overflow-auto">
        {items.length === 0 ? (
          <div className={`p-4 text-sm ${classes.muted}`}>No {resourceLabel(resourceView).toLowerCase()} matched this scope or search.</div>
        ) : (
          items.map((item) => {
            const key = resourceKey(resourceView, item);
            const active =
              resourceView === "containers"
                ? selectedContainer && (selectedContainer.id === item.id || selectedContainer.name === item.name)
                : selectedResourceID === key;
            return (
              <button
                type="button"
                key={key}
                className={`grid w-full gap-1 border-b px-3 py-3 text-left text-sm ${classes.border} ${classes.rowHover} ${active ? classes.activeRow : ""}`}
                onClick={() => onSelect(item)}
                aria-pressed={Boolean(active)}
              >
                <span className="flex min-w-0 items-center justify-between gap-3">
                  <span className="truncate font-semibold">{resourcePrimary(resourceView, item)}</span>
                  <Badge tone={resourceTone(resourceView, item)}>{resourceStatus(resourceView, item)}</Badge>
                </span>
                <span className={`truncate text-xs ${classes.muted}`}>{resourceSecondary(resourceView, item)}</span>
                <span className={`truncate text-xs ${classes.muted}`}>{resourceTertiary(resourceView, item)}</span>
              </button>
            );
          })
        )}
      </div>
    </section>
  );
}
