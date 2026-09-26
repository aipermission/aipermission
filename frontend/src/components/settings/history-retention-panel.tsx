import { Clock3, RefreshCcw } from "lucide-react";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import {
  retentionPurgeResponse,
  retentionSettingsResponse,
  type RetentionSettings,
} from "../../lib/gateway-contracts/retention-settings-contract";
import { useRequestGuard } from "../../lib/request-guard";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const defaultRetention: RetentionSettings = { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 };
type RetentionResource = { state: "loading" | "ready" | "error"; data: RetentionSettings; error: string | null };
const purgeOptions = [
  ["history", 30, "Purge history older than 30 days"],
  ["audit", 30, "Purge audit older than 30 days"],
  ["console", 7, "Purge consoles older than 7 days"],
  ["messages", 7, "Purge messages older than 7 days"],
] as const;

export function HistoryRetentionPanel() {
  const [retention, setRetention] = useState<RetentionResource>({ state: "loading", data: defaultRetention, error: null });
  const requestGuard = useRequestGuard("retention-settings");
  const { actionState: saveState, runAction: runSave } = useAsyncAction();
  const { actionState: purgeState, runAction: runPurge } = useAsyncAction();
  const busy = saveState.state === "saving" || purgeState.state === "purging";

  const loadRetention = useCallback(async () => {
    const request = requestGuard.begin("settings");
    setRetention((current) => ({ ...current, state: "loading", error: null }));
    try {
      const data = retentionSettingsResponse(await apiGet("/api/settings/retention", { signal: request.signal }));
      if (request.isCurrent()) setRetention({ state: "ready", data, error: null });
    } catch (error) {
      if (request.isCurrent())
        setRetention((current) => ({ ...current, state: "error", error: errorMessage(error, "Unable to load retention settings.") }));
    } finally {
      request.complete();
    }
  }, [requestGuard]);

  useEffect(() => {
    void loadRetention();
  }, [loadRetention]);

  function updateField(field: keyof RetentionSettings, value: string) {
    if (retention.state !== "ready" || busy) return;
    const numeric = Number.parseInt(value, 10);
    setRetention((current) => ({
      ...current,
      data: { ...current.data, [field]: Number.isFinite(numeric) && numeric >= 0 ? numeric : 0 },
    }));
  }

  async function saveRetention(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (retention.state !== "ready" || busy) return;
    await runSave({
      pending: "saving",
      successMessage: "Retention settings saved and cleanup ran.",
      action: async () => {
        const request = requestGuard.begin("settings");
        try {
          const data = retentionSettingsResponse(await apiPut("/api/settings/retention", retention.data));
          if (request.isCurrent()) setRetention({ state: "ready", data, error: null });
        } finally {
          request.complete();
        }
      },
    });
  }

  async function purgeRetention(target: (typeof purgeOptions)[number][0], days: number) {
    if (busy) return;
    if (!window.confirm(`Delete ${target} records older than ${days} days? This cannot be undone.`)) return;
    await runPurge({
      pending: "purging",
      successMessage: (data) => `Deleted ${data.deleted} ${target} records.`,
      action: async () => retentionPurgeResponse(await apiPost("/api/settings/retention/purge", { target, days })),
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Data retention</CardTitle>
        <CardDescription>Keep History and Audit usable by cleaning old local records.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4" onSubmit={saveRetention}>
          <Notice>
            Cleanup runs when a database is unlocked, after saving these settings, and hourly while it remains unlocked. Use 0 to disable
            automatic cleanup for a category.
          </Notice>
          {retention.state === "error" ? (
            <div className="flex flex-wrap items-center gap-2" role="alert">
              <Notice tone="bad">{retention.error}</Notice>
              <Button type="button" variant="outline" onClick={loadRetention}>
                <RefreshCcw className="h-4 w-4" />
                Retry
              </Button>
            </div>
          ) : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <RetentionField
              label="Command history days"
              value={retention.data.history_days}
              disabled={busy}
              onChange={(value) => updateField("history_days", value)}
            />
            <RetentionField
              label="Audit log days"
              value={retention.data.audit_days}
              disabled={busy}
              onChange={(value) => updateField("audit_days", value)}
            />
            <RetentionField
              label="Console session days"
              value={retention.data.console_days}
              disabled={busy}
              onChange={(value) => updateField("console_days", value)}
            />
            <RetentionField
              label="Message days"
              value={retention.data.message_days}
              disabled={busy}
              onChange={(value) => updateField("message_days", value)}
            />
          </div>
          <Button type="submit" variant="outline" disabled={busy || retention.state !== "ready"}>
            <Clock3 className="h-4 w-4" />
            {saveState.state === "saving" ? "Saving..." : "Save retention"}
          </Button>
          {saveState.message ? <Notice tone="good">{saveState.message}</Notice> : null}
          {saveState.state === "error" ? <Notice tone="bad">{saveState.error}</Notice> : null}
          <div className="grid gap-3 rounded-md border border-stone-200 p-3">
            <div>
              <h3 className="text-sm font-semibold text-stone-900">Manual cleanup</h3>
              <p className="text-xs text-stone-500">Run a one-time purge without changing automatic retention settings.</p>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {purgeOptions.map(([target, days, label]) => (
                <Button type="button" variant="outline" onClick={() => purgeRetention(target, days)} disabled={busy} key={target}>
                  {label}
                </Button>
              ))}
            </div>
            {purgeState.message ? <Notice tone="good">{purgeState.message}</Notice> : null}
            {purgeState.state === "error" ? <Notice tone="bad">{purgeState.error}</Notice> : null}
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function RetentionField({
  label,
  value,
  disabled,
  onChange,
}: {
  label: string;
  value: number;
  disabled: boolean;
  onChange: (_value: string) => void;
}) {
  return (
    <Field>
      {label}
      <Input type="number" min="0" step="1" value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)} />
    </Field>
  );
}
