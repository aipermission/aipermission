import { Clock3 } from "lucide-react";
import { useEffect, useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const defaultRetention = { history_days: 0, audit_days: 0, console_days: 0, message_days: 0 };

export function HistoryRetentionPanel() {
  const [retention, setRetention] = useState({ state: "loading", data: defaultRetention, error: null });
  const { actionState: saveState, runAction: runSave } = useAsyncAction();
  const { actionState: purgeState, runAction: runPurge } = useAsyncAction();

  useEffect(() => {
    void loadRetention();
  }, []);

  async function loadRetention() {
    try {
      const data = await apiGet("/api/settings/retention");
      setRetention({ state: "ready", data, error: null });
    } catch (error) {
      setRetention((current) => ({ ...current, state: "error", error: error.message }));
    }
  }

  function updateField(field, value) {
    const numeric = Number.parseInt(value, 10);
    setRetention((current) => ({
      ...current,
      data: { ...current.data, [field]: Number.isFinite(numeric) && numeric >= 0 ? numeric : 0 },
    }));
  }

  async function saveRetention(event) {
    event.preventDefault();
    await runSave({
      pending: "saving",
      successMessage: "Retention settings saved and cleanup ran.",
      action: async () => {
        const data = await apiPut("/api/settings/retention", retention.data);
        setRetention({ state: "ready", data, error: null });
      },
    });
  }

  async function purgeRetention(target, days) {
    if (!window.confirm(`Delete ${target} records older than ${days} days? This cannot be undone.`)) return;
    await runPurge({
      pending: "purging",
      successMessage: (data) => `Deleted ${data.deleted} ${target} records.`,
      action: () => apiPost("/api/settings/retention/purge", { target, days }),
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
            Cleanup runs when a database is unlocked and immediately after saving these settings. Use 0 to disable automatic cleanup for a
            category.
          </Notice>
          {retention.state === "error" ? <Notice tone="bad">{retention.error}</Notice> : null}
          <div className="grid gap-3 sm:grid-cols-2">
            <RetentionField
              label="Command history days"
              value={retention.data.history_days}
              onChange={(value) => updateField("history_days", value)}
            />
            <RetentionField
              label="Audit log days"
              value={retention.data.audit_days}
              onChange={(value) => updateField("audit_days", value)}
            />
            <RetentionField
              label="Console session days"
              value={retention.data.console_days}
              onChange={(value) => updateField("console_days", value)}
            />
            <RetentionField
              label="Message days"
              value={retention.data.message_days}
              onChange={(value) => updateField("message_days", value)}
            />
          </div>
          <Button type="submit" variant="outline" disabled={saveState.state === "saving" || retention.state === "loading"}>
            <Clock3 className="h-4 w-4" />
            {saveState.state === "saving" ? "Saving..." : "Save retention"}
          </Button>
          {saveState.message ? <Notice tone="good">{saveState.message}</Notice> : null}
          {saveState.state === "error" ? <Notice tone="bad">{saveState.error}</Notice> : null}
          <div className="grid gap-3 rounded-md border border-stone-200 p-3">
            <div>
              <h4 className="text-sm font-semibold text-stone-900">Manual cleanup</h4>
              <p className="text-xs text-stone-500">Run a one-time purge without changing automatic retention settings.</p>
            </div>
            <div className="grid gap-2 sm:grid-cols-2">
              {[
                ["history", 30, "Purge history older than 30 days"],
                ["audit", 30, "Purge audit older than 30 days"],
                ["console", 7, "Purge consoles older than 7 days"],
                ["messages", 7, "Purge messages older than 7 days"],
              ].map(([target, days, label]) => (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => purgeRetention(target, days)}
                  disabled={purgeState.state === "purging"}
                  key={target}
                >
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

function RetentionField({ label, value, onChange }) {
  return (
    <Field>
      {label}
      <Input type="number" min="0" step="1" value={value} onChange={(event) => onChange(event.target.value)} />
    </Field>
  );
}
