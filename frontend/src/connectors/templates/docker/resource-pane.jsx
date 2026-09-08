import { FileJson, LoaderCircle, Play, RefreshCcw, RotateCcw, Square, TerminalSquare } from "lucide-react";
import { Button } from "../../../components/ui/button";
import { Input } from "../../../components/ui/form";
import { DockerContainerConsolePanel } from "./container-console-panel";
import { resourcePlaceholder, resourcePrimary, resourceSecondary, resourceSingular } from "./helpers";
import { DockerResourceDetail, DockerResultView } from "./result-view";

export function DockerResourcePane({
  children,
  resourceView,
  selectedResource,
  selectedContainer,
  containerRef,
  viewMode,
  result,
  resultSearch,
  tail,
  state,
  target,
  selectedRuntimeTarget,
  session,
  sessionLive,
  consolePending,
  theme,
  classes,
  onTailChange,
  onResultSearch,
  onReadLogs,
  onInspect,
  onOpenConsole,
  onStartConsole,
  onEndConsole,
  onLifecycle,
}) {
  const showingInspect = viewMode === "inspect";
  return (
    <section
      className={`grid h-full min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border ${classes.border} ${classes.subtlePanel}`}
    >
      <DockerResourcePaneHeader
        resourceView={resourceView}
        selectedResource={selectedResource}
        selectedContainer={selectedContainer}
        viewMode={viewMode}
        showingInspect={showingInspect}
        tail={tail}
        state={state}
        classes={classes}
        onTailChange={onTailChange}
        onReadLogs={onReadLogs}
        onInspect={onInspect}
        onOpenConsole={onOpenConsole}
        onLifecycle={onLifecycle}
      />
      <DockerResourcePaneContent
        resourceView={resourceView}
        selectedResource={selectedResource}
        selectedContainer={selectedContainer}
        containerRef={containerRef}
        viewMode={viewMode}
        showingInspect={showingInspect}
        result={result}
        resultSearch={resultSearch}
        state={state}
        target={target}
        selectedRuntimeTarget={selectedRuntimeTarget}
        session={session}
        sessionLive={sessionLive}
        consolePending={consolePending}
        theme={theme}
        classes={classes}
        onResultSearch={onResultSearch}
        onStartConsole={onStartConsole}
        onEndConsole={onEndConsole}
      >
        {children}
      </DockerResourcePaneContent>
    </section>
  );
}

function DockerResourcePaneHeader({
  resourceView,
  selectedResource,
  selectedContainer,
  viewMode,
  showingInspect,
  tail,
  state,
  classes,
  onTailChange,
  onReadLogs,
  onInspect,
  onOpenConsole,
  onLifecycle,
}) {
  return (
    <div>
      <div className={`border-b p-3 ${classes.border}`}>
        <div className="flex min-w-0 flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="flex min-w-0 items-center gap-2">
              <p className="truncate text-sm font-semibold">
                {selectedResource ? resourcePrimary(resourceView, selectedResource) : `Select ${resourceSingular(resourceView)}`}
              </p>
              {state.state !== "idle" ? (
                <span className={`inline-flex shrink-0 items-center gap-1 text-xs ${classes.muted}`}>
                  <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                  Loading
                </span>
              ) : null}
            </div>
            <p className={`truncate text-xs ${classes.muted}`}>
              {selectedResource ? resourceSecondary(resourceView, selectedResource) : resourcePlaceholder(resourceView)}
            </p>
          </div>
          {resourceView === "containers" && selectedContainer ? (
            <DockerContainerToolbar
              viewMode={viewMode}
              showingInspect={showingInspect}
              tail={tail}
              disabled={state.state !== "idle"}
              inputClass={classes.input}
              onTailChange={onTailChange}
              onReadLogs={onReadLogs}
              onInspect={onInspect}
              onOpenConsole={onOpenConsole}
              onLifecycle={onLifecycle}
            />
          ) : null}
        </div>
      </div>
      {state.error ? (
        <div className={`border-b px-3 py-2 text-right text-xs text-red-500 ${classes.border}`}>
          <span className="break-words">{state.error}</span>
        </div>
      ) : null}
    </div>
  );
}

function DockerContainerToolbar({
  viewMode,
  showingInspect,
  tail,
  disabled,
  inputClass,
  onTailChange,
  onReadLogs,
  onInspect,
  onOpenConsole,
  onLifecycle,
}) {
  return (
    <div className="flex flex-wrap items-center justify-end gap-2">
      <Button
        type="button"
        variant="outline"
        className="h-8 px-2 text-xs"
        onClick={showingInspect ? onReadLogs : onInspect}
        disabled={disabled}
        title={showingInspect ? "Show container logs" : "Inspect container"}
      >
        {showingInspect ? <RefreshCcw className="h-3.5 w-3.5" /> : <FileJson className="h-3.5 w-3.5" />}
        {showingInspect ? "Logs" : "Inspect"}
      </Button>
      {!showingInspect && viewMode !== "console" ? (
        <>
          <label className="flex items-center gap-2 text-xs font-semibold uppercase tracking-wide">
            Tail
            <Input
              className={`h-8 w-24 ${inputClass}`}
              type="number"
              min="1"
              max="2000"
              value={tail}
              onChange={(event) => onTailChange(event.target.value)}
            />
          </label>
          <IconButton label="Refresh logs" disabled={disabled} onClick={onReadLogs} icon={RefreshCcw} />
        </>
      ) : null}
      <IconButton label="Open live console inside this container" disabled={disabled} onClick={onOpenConsole} icon={TerminalSquare} />
      <IconButton label="Start container" disabled={disabled} onClick={() => onLifecycle("start_container")} icon={Play} />
      <IconButton label="Stop container" disabled={disabled} onClick={() => onLifecycle("stop_container")} icon={Square} />
      <IconButton label="Restart container" disabled={disabled} onClick={() => onLifecycle("restart_container")} icon={RotateCcw} />
    </div>
  );
}

function IconButton({ label, disabled, onClick, icon: Icon }) {
  return (
    <Button type="button" variant="outline" className="h-8 w-8 px-0" onClick={onClick} disabled={disabled} title={label}>
      <Icon className="h-3.5 w-3.5" />
    </Button>
  );
}

function DockerResourcePaneContent({
  children,
  resourceView,
  selectedResource,
  selectedContainer,
  containerRef,
  viewMode,
  showingInspect,
  result,
  resultSearch,
  state,
  target,
  selectedRuntimeTarget,
  session,
  sessionLive,
  consolePending,
  theme,
  classes,
  onResultSearch,
  onStartConsole,
  onEndConsole,
}) {
  let content;
  if (!selectedResource) {
    content = <EmptyPane classes={classes}>{resourcePlaceholder(resourceView)}</EmptyPane>;
  } else if (resourceView !== "containers") {
    content = (
      <DockerResourceDetail
        resourceView={resourceView}
        item={selectedResource}
        search={resultSearch}
        onSearch={onResultSearch}
        inputClass={classes.input}
      />
    );
  } else if (viewMode === "console") {
    content = (
      <DockerContainerConsolePanel
        target={target}
        container={selectedContainer}
        containerRef={containerRef}
        selectedRuntimeTarget={selectedRuntimeTarget}
        session={session}
        sessionLive={sessionLive}
        pending={consolePending}
        theme={theme}
        mutedClass={classes.muted}
        borderClass={classes.border}
        onStart={onStartConsole}
        onEnd={onEndConsole}
      >
        {children}
      </DockerContainerConsolePanel>
    );
  } else if (state.state !== "idle" && !result) {
    content = (
      <EmptyPane classes={classes} fill>
        <span className="inline-flex items-center gap-2">
          <LoaderCircle className="h-4 w-4 animate-spin" />
          Loading {showingInspect ? "inspect metadata" : "logs"} for {selectedContainer.name || selectedContainer.id}...
        </span>
      </EmptyPane>
    );
  } else if (result) {
    content = <DockerResultView item={result} search={resultSearch} onSearch={onResultSearch} inputClass={classes.input} />;
  } else {
    content = <EmptyPane classes={classes}>Logs will appear here after the container is loaded.</EmptyPane>;
  }

  return <div className="grid h-full min-h-0 grid-rows-[minmax(0,1fr)] overflow-hidden p-3">{content}</div>;
}

function EmptyPane({ children, classes, fill = false }) {
  return (
    <div
      className={`grid place-items-center rounded-lg border border-dashed p-8 text-center text-sm ${fill ? "h-full min-h-0" : ""} ${classes.border} ${classes.muted}`}
    >
      {children}
    </div>
  );
}
