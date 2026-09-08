import { Plus, RefreshCcw, Save } from "lucide-react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Input, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";
import { formatRedisValue, keyMetaText } from "./browser-helpers";

export function RedisValueWorkspace({ browser, styles }) {
  return (
    <section className={`grid min-h-0 grid-rows-[auto_auto_minmax(0,1fr)_auto] overflow-hidden rounded-lg border ${styles.border}`}>
      <ValueHeader browser={browser} styles={styles} />
      <ValueToolbar browser={browser} styles={styles} />
      <div className="min-h-0 overflow-hidden p-4">
        <ValueContent browser={browser} inputClass={styles.input} />
      </div>
      <footer className={`grid gap-2 border-t p-3 ${styles.border}`}>
        {browser.keyResult && browser.keyResult.type !== "string" && browser.resultMode === "value" ? (
          <Notice tone="warn">
            This {browser.product} type is read-only in the MVP. TTL changes are still available from the toolbar.
          </Notice>
        ) : null}
        {browser.state.error ? <Notice tone="bad">{browser.state.error}</Notice> : null}
        {browser.state.message ? <Notice tone="good">{browser.state.message}</Notice> : null}
      </footer>
    </section>
  );
}

function ValueHeader({ browser, styles }) {
  return (
    <header className={`flex flex-wrap items-center justify-between gap-3 border-b p-3 ${styles.border} ${styles.subtlePanel}`}>
      <div className="min-w-0">
        <p className="text-sm font-semibold">{browser.activeKey || "New string key"}</p>
        <p className={`truncate text-xs ${styles.muted}`}>
          {browser.keyResult
            ? keyMetaText(browser.keyResult)
            : browser.creatingKey
              ? `Create a ${browser.product} string value.`
              : "Select a key from the browser."}
        </p>
      </div>
      <div className="flex items-center gap-2">
        {browser.keyResult?.type ? (
          <Badge tone={browser.keyResult.type === "none" ? "neutral" : "good"}>{browser.keyResult.type}</Badge>
        ) : null}
        {browser.keyResult ? (
          <CopyButton
            value={JSON.stringify(browser.keyResult, null, 2)}
            variant="outline"
            className="h-8 px-2 text-xs"
            title="Copy key JSON"
          >
            JSON
          </CopyButton>
        ) : null}
      </div>
    </header>
  );
}

function ValueToolbar({ browser, styles }) {
  return (
    <div className={`flex flex-wrap items-center justify-between gap-2 border-b p-3 ${styles.border}`}>
      <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
        <Button
          type="button"
          variant={browser.resultMode === "value" ? "default" : "outline"}
          className="h-8 px-3 text-xs"
          onClick={() => browser.setResultMode("value")}
        >
          Value
        </Button>
        <Button
          type="button"
          variant={browser.resultMode === "json" ? "default" : "outline"}
          className="h-8 px-3 text-xs"
          onClick={() => browser.setResultMode("json")}
        >
          Raw JSON
        </Button>
        {browser.activeKey ? (
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            title="Reload key"
            aria-label="Reload key"
            onClick={() => browser.loadKey(browser.activeKey)}
            disabled={browser.state.state !== "idle"}
          >
            <RefreshCcw className="h-3.5 w-3.5" />
          </Button>
        ) : null}
        {browser.creatingKey ? (
          <Input
            className={`h-8 min-w-56 flex-1 ${styles.input}`}
            value={browser.newKey}
            onChange={(event) => browser.setNewKey(event.target.value)}
            placeholder="New key name"
            aria-label="New key name"
          />
        ) : null}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
        <div className="flex items-center gap-1">
          <Input
            className={`h-8 w-28 ${styles.input}`}
            value={browser.ttlDraft}
            onChange={(event) => browser.setTTLDraft(event.target.value)}
            placeholder="TTL"
            aria-label="TTL seconds"
          />
          <Button
            type="button"
            variant="outline"
            className="h-8 w-8 px-0"
            title="Save TTL"
            aria-label="Save TTL"
            disabled={!browser.canUpdateTTL}
            onClick={browser.updateTTL}
          >
            <Save className="h-3.5 w-3.5" />
          </Button>
        </div>
        <Button
          type="button"
          className="h-8 px-3 text-xs"
          disabled={browser.state.state !== "idle" || !browser.canSaveString}
          onClick={browser.saveStringValue}
          title={browser.editableString ? `Save ${browser.product} string value` : `This ${browser.product} type is read-only in the MVP`}
        >
          {browser.activeKey ? <Save className="h-3.5 w-3.5" /> : <Plus className="h-3.5 w-3.5" />}
          {browser.activeKey ? (browser.editableString ? "Save string" : "Read only") : "Create key"}
        </Button>
      </div>
    </div>
  );
}

function ValueContent({ browser, inputClass }) {
  if (browser.resultMode === "json")
    return (
      <TerminalBlock surface="log" className="h-full min-h-0 text-xs">
        {browser.keyResult
          ? JSON.stringify(browser.keyResult, null, 2)
          : browser.creatingKey
            ? "New key is not saved yet."
            : "No key selected."}
      </TerminalBlock>
    );
  if (browser.creatingKey)
    return (
      <Textarea
        className={`h-full min-h-0 resize-none font-mono text-xs ${inputClass}`}
        value={browser.newValue}
        onChange={(event) => browser.setNewValue(event.target.value)}
        placeholder="String value"
        aria-label="String value"
      />
    );
  if (!browser.keyResult) return <Notice>Select a key from the left panel to inspect its value.</Notice>;
  if (browser.keyResult.type === "string")
    return (
      <Textarea
        className={`h-full min-h-0 resize-none font-mono text-xs ${inputClass}`}
        value={browser.valueDraft}
        onChange={(event) => browser.setValueDraft(event.target.value)}
        aria-label="String value"
      />
    );
  return (
    <TerminalBlock surface="log" className="min-h-0 text-xs">
      {formatRedisValue(browser.keyResult.value)}
    </TerminalBlock>
  );
}
