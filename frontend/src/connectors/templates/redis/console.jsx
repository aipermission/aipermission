import { Database, Plus, RefreshCcw, Save, Search, Trash2 } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Checkbox, Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { connectorConsoleTheme } from "../_shared/console-theme";
import { serverProductLabel } from "./model";
import { formatRedisValue, keyMetaText, useRedisBrowser } from "./use-redis-browser";

export function RedisConnectorConsoleTemplate({ target, approvals, theme, session, onNewStructuredSession, onRefreshActivity }) {
  const browser = useRedisBrowser({ target, approvals, session, onRefreshActivity });
  const {
    activeSession,
    product,
    pattern,
    setPattern,
    cursor,
    keys,
    selectedKeys,
    setSelectedKeys,
    selectedCount,
    activeKey,
    keyResult,
    valueDraft,
    setValueDraft,
    newKey,
    setNewKey,
    newValue,
    setNewValue,
    ttlDraft,
    setTTLDraft,
    state,
    resultMode,
    setResultMode,
    confirmDialog,
    closeConfirmDialog,
    latestAction,
    creatingKey,
    editableString,
    canSaveString,
    canUpdateTTL,
    scanKeys,
    startNewKey,
    loadKey,
    saveStringValue,
    updateTTL,
    deleteSelected,
    confirmPendingAction,
    toggleSelection,
  } = browser;
  const {
    panel: panelClass,
    muted: mutedClass,
    border: borderClass,
    subtlePanel: subtlePanelClass,
    input: inputClass,
    rowHover: rowHoverClass,
    activeRow: activeRowClass,
  } = connectorConsoleTheme(theme);
  if (!activeSession.active) {
    return (
      <div className={`grid min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
        <div className="grid place-items-center p-8 text-center">
          <div className="grid max-w-lg gap-4">
            <Database className={`mx-auto h-10 w-10 ${mutedClass}`} />
            <div>
              <h3 className="text-lg font-semibold">No active {product} session</h3>
              <p className={`mt-2 text-sm ${mutedClass}`}>
                Start a structured session to browse {product} keys through the connector approval, history, and audit pipeline.
              </p>
            </div>
            <Button type="button" className="mx-auto" onClick={onNewStructuredSession}>
              Start {product} session
            </Button>
          </div>
        </div>
        <RedisEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      </div>
    );
  }

  return (
    <div className={`grid h-full min-h-0 grid-rows-[minmax(0,1fr)_auto] ${panelClass}`}>
      <div className="grid min-h-0 gap-4 overflow-hidden p-4 lg:grid-cols-[340px_minmax(0,1fr)]">
        <section
          className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass} ${subtlePanelClass}`}
        >
          <div className={`border-b p-3 ${borderClass}`}>
            <div className="flex flex-wrap items-center justify-between gap-2">
              <div>
                <p className="text-sm font-semibold">Keys</p>
                <p className={`text-xs ${mutedClass}`}>{keys.length} loaded</p>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                {latestAction ? (
                  <Badge tone={latestAction.status === "failed" ? "bad" : latestAction.status === "completed" ? "good" : "warn"}>
                    {latestAction.action_name}
                  </Badge>
                ) : null}
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Refresh keys"
                  onClick={() => scanKeys({ reset: true })}
                  disabled={state.state !== "idle"}
                >
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
                <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={startNewKey}>
                  <Plus className="h-3.5 w-3.5" />
                  New
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 px-2 text-xs"
                  onClick={() => setSelectedKeys(selectedKeys.length === keys.length ? [] : keys)}
                >
                  {selectedKeys.length === keys.length && keys.length > 0 ? "None" : "All"}
                </Button>
              </div>
            </div>
          </div>
          <form
            className={`grid gap-2 border-b p-3 ${borderClass}`}
            onSubmit={(event) => {
              event.preventDefault();
              void scanKeys({ reset: true });
            }}
          >
            <div className="relative">
              <Search className={`pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 ${mutedClass}`} />
              <Input
                className={`pl-9 ${inputClass}`}
                value={pattern}
                onChange={(event) => setPattern(event.target.value)}
                placeholder="SCAN pattern, e.g. user:*"
              />
            </div>
            <Button type="submit" variant="outline" className="h-9" disabled={state.state !== "idle"}>
              {state.state === "scanning" ? "Scanning" : "Scan keys"}
            </Button>
          </form>
          <div className="min-h-0 overflow-auto p-2">
            {keys.map((key) => (
              <button
                key={key}
                type="button"
                className={`mb-1 grid w-full grid-cols-[auto_minmax(0,1fr)] items-center gap-2 rounded-md border px-2 py-2 text-left text-sm transition ${activeKey === key ? activeRowClass : `${borderClass} ${rowHoverClass}`}`}
                onClick={() => loadKey(key)}
              >
                <Checkbox
                  checked={selectedKeys.includes(key)}
                  onClick={(event) => event.stopPropagation()}
                  onChange={() => toggleSelection(key)}
                  aria-label={`Select ${key}`}
                />
                <span className="truncate font-mono text-xs" title={key}>
                  {key}
                </span>
              </button>
            ))}
            {keys.length === 0 ? (
              <Notice>
                {state.state === "scanning" ? `Scanning ${product} keys...` : "No keys loaded. Scan to browse this database."}
              </Notice>
            ) : null}
          </div>
          <div className={`flex items-center justify-between gap-2 border-t p-3 ${borderClass}`}>
            <Button
              type="button"
              variant="outline"
              className="h-8 px-3 text-xs"
              disabled={cursor === "0" || state.state !== "idle"}
              onClick={() => scanKeys({ reset: false })}
            >
              More
            </Button>
            <Button
              type="button"
              variant="outline"
              className="h-8 px-3 text-xs text-red-600"
              disabled={(selectedCount === 0 && !activeKey) || state.state !== "idle"}
              onClick={deleteSelected}
            >
              <Trash2 className="h-3.5 w-3.5" />
              Delete {selectedCount || (activeKey ? 1 : "")}
            </Button>
          </div>
        </section>

        <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${borderClass}`}>
          <div className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${borderClass} ${subtlePanelClass}`}>
            <div className="min-w-0">
              <p className="text-sm font-semibold">{activeKey || "New string key"}</p>
              <p className={`truncate text-xs ${mutedClass}`}>
                {keyResult ? keyMetaText(keyResult) : creatingKey ? `Create a ${product} string value.` : "Select a key from the browser."}
              </p>
            </div>
            <div className="flex items-center gap-2">
              {keyResult?.type ? <Badge tone={keyResult.type === "none" ? "neutral" : "good"}>{keyResult.type}</Badge> : null}
              {keyResult ? (
                <CopyButton value={JSON.stringify(keyResult, null, 2)} variant="outline" className="h-8 px-2 text-xs" title="Copy key JSON">
                  JSON
                </CopyButton>
              ) : null}
            </div>
          </div>
          <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${borderClass}`}>
            <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
              <Button
                type="button"
                variant={resultMode === "value" ? "default" : "outline"}
                className="h-8 px-3 text-xs"
                onClick={() => setResultMode("value")}
              >
                Value
              </Button>
              <Button
                type="button"
                variant={resultMode === "json" ? "default" : "outline"}
                className="h-8 px-3 text-xs"
                onClick={() => setResultMode("json")}
              >
                Raw JSON
              </Button>
              {activeKey ? (
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Reload key"
                  onClick={() => loadKey(activeKey)}
                  disabled={state.state !== "idle"}
                >
                  <RefreshCcw className="h-3.5 w-3.5" />
                </Button>
              ) : null}
              {creatingKey ? (
                <Input
                  className={`h-8 min-w-56 flex-1 ${inputClass}`}
                  value={newKey}
                  onChange={(event) => setNewKey(event.target.value)}
                  placeholder="New key name"
                />
              ) : null}
            </div>
            <div className="flex flex-wrap items-center justify-end gap-2">
              <div className="flex items-center gap-1">
                <Input
                  className={`h-8 w-28 ${inputClass}`}
                  value={ttlDraft}
                  onChange={(event) => setTTLDraft(event.target.value)}
                  placeholder="TTL"
                  aria-label="TTL seconds"
                />
                <Button
                  type="button"
                  variant="outline"
                  className="h-8 w-8 px-0"
                  title="Save TTL"
                  disabled={!canUpdateTTL}
                  onClick={updateTTL}
                >
                  <Save className="h-3.5 w-3.5" />
                </Button>
              </div>
              <Button
                type="button"
                className="h-8 px-3 text-xs"
                disabled={state.state !== "idle" || !canSaveString}
                onClick={saveStringValue}
                title={editableString ? `Save ${product} string value` : `This ${product} type is read-only in the MVP`}
              >
                {activeKey ? <Save className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
                {activeKey ? (editableString ? "Save string" : "Read only") : "Create key"}
              </Button>
            </div>
          </div>
          <div className="min-h-0 overflow-hidden p-4">
            {resultMode === "json" ? (
              <TerminalBlock surface="log" className="h-full min-h-0 text-xs">
                {keyResult ? JSON.stringify(keyResult, null, 2) : creatingKey ? "New key is not saved yet." : "No key selected."}
              </TerminalBlock>
            ) : creatingKey ? (
              <Textarea
                className={`h-full min-h-0 resize-none font-mono text-xs ${inputClass}`}
                value={newValue}
                onChange={(event) => setNewValue(event.target.value)}
                placeholder="String value"
              />
            ) : (
              <ValuePanel keyResult={keyResult} valueDraft={valueDraft} onValueDraft={setValueDraft} inputClass={inputClass} />
            )}
          </div>
          <div className={`grid gap-2 border-t p-3 ${borderClass}`}>
            {keyResult && keyResult.type !== "string" && resultMode === "value" ? (
              <Notice tone="warn">This {product} type is read-only in the MVP. TTL changes are still available from the toolbar.</Notice>
            ) : null}
            {state.error ? <Notice tone="bad">{state.error}</Notice> : null}
            {state.message ? <Notice tone="good">{state.message}</Notice> : null}
          </div>
        </section>
      </div>

      <RedisEndpointFooter target={target} borderClass={borderClass} mutedClass={mutedClass} />
      <RedisConfirmDialog
        value={confirmDialog}
        theme={theme}
        product={product}
        onClose={closeConfirmDialog}
        onConfirm={confirmPendingAction}
      />
    </div>
  );
}

function RedisConfirmDialog({ value, theme, product, onClose, onConfirm }) {
  const danger = value.tone === "bad";
  const noticeTone = danger ? "bad" : "warn";
  const detailClass = theme === "light" ? "bg-stone-50" : "bg-stone-900/70 text-stone-100";
  return (
    <Dialog
      open={value.open}
      title={value.title}
      description={value.description}
      onClose={onClose}
      closeDisabled={value.pending}
      size="md"
      closeOnOverlay={!value.pending}
      closeOnEscape={!value.pending}
      className={theme === "light" ? "" : "border-stone-700 bg-[#252526] text-stone-100"}
      bodyClassName={theme === "light" ? "" : "bg-[#252526]"}
    >
      <div className="grid gap-4">
        <Notice tone={noticeTone}>{danger ? "This operation cannot be undone." : `Review the ${product} write before continuing.`}</Notice>
        {value.error ? <Notice tone="bad">{value.error}</Notice> : null}
        {value.details?.length ? (
          <div className={`max-h-56 overflow-auto rounded-md border border-stone-300 p-3 text-sm ${detailClass}`}>
            {value.details.map((item, index) => (
              <div key={`${item.label}:${index}`} className="grid gap-1 py-1 sm:grid-cols-[110px_minmax(0,1fr)]">
                <span className="text-xs font-semibold uppercase text-stone-500">{item.label}</span>
                <span className="break-all font-mono text-xs">{item.value}</span>
              </div>
            ))}
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="button" variant={danger ? "danger" : "default"} onClick={onConfirm} disabled={value.pending}>
            {value.pending ? "Working..." : danger ? "Delete" : "Confirm"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}

function ValuePanel({ keyResult, valueDraft, onValueDraft, inputClass }) {
  if (!keyResult) {
    return <Notice>Select a key from the left panel to inspect its value.</Notice>;
  }
  if (keyResult.type === "string") {
    return (
      <Textarea
        className={`min-h-0 h-full resize-none font-mono text-xs ${inputClass}`}
        value={valueDraft}
        onChange={(event) => onValueDraft(event.target.value)}
      />
    );
  }
  return (
    <TerminalBlock surface="log" className="min-h-0 text-xs">
      {formatRedisValue(keyResult.value)}
    </TerminalBlock>
  );
}

function RedisEndpointFooter({ target, borderClass, mutedClass }) {
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 border-t px-3 py-2 text-xs ${borderClass}`}>
      <span className={`truncate font-mono ${mutedClass}`}>{target.ref}</span>
      <span className={`truncate ${mutedClass}`}>
        {serverProductLabel(target)} · {target.config?.host}:{target.config?.port} db {target.config?.database || 0}
      </span>
    </div>
  );
}
