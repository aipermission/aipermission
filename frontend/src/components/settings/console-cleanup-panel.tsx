import { useCallback, useEffect, useRef, useState } from "react";
import { RefreshCw, Save } from "lucide-react";
import { apiGet, apiPut, currentWorkspaceBinding } from "../../lib/api";
import { objectRecord } from "../../lib/api-types";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";
import { subscribeUISessionInvalidation } from "../../lib/ui-session-events";
import type { components } from "../../../types/generated-openapi";
import { idleOptions, idleLabel } from "../capacity/model";
import { Button } from "../ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";
import { Field, Select } from "../ui/form";
import { Notice } from "../ui/notice";

type Policy = components["schemas"]["ConsoleCleanupPolicy"];
function policyResponse(value: unknown): Policy {
  const data = objectRecord(value);
  if (!data || typeof data.enabled !== "boolean" || !idleOptions.some((minutes) => minutes === data.idle_minutes))
    throw new Error("Invalid console cleanup settings.");
  return data as Policy;
}

export function ConsoleCleanupPanel() {
  const [policy, setPolicy] = useState<Policy | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const busy = useRef(false);
  const blocked = useRef(false);
  const policyWorkspace = useRef("");
  const guard = useRequestGuard("console-cleanup-settings");
  const load = useCallback(async () => {
    if (busy.current || blocked.current) return;
    const workspace = currentWorkspaceBinding();
    const request = guard.begin("policy");
    setPolicy(null);
    setError(null);
    setMessage(null);
    try {
      const data = policyResponse(await apiGet("/api/settings/console-cleanup", { signal: request.signal, timeoutMs: 4000 }));
      if (request.isCurrent() && currentWorkspaceBinding() === workspace) {
        policyWorkspace.current = workspace;
        setPolicy(data);
      }
    } catch (error) {
      if (request.isCurrent()) setError(errorMessage(error, "Could not load console cleanup settings."));
    } finally {
      request.complete();
    }
  }, [guard]);
  useEffect(() => {
    void load();
  }, [load]);
  useEffect(
    () =>
      subscribeUISessionInvalidation(() => {
        blocked.current = true;
        guard.invalidate("policy");
        setPolicy(null);
        setSaving(false);
        setMessage(null);
        setError("The database session changed. Unlock or reload before changing cleanup settings.");
      }),
    [guard],
  );

  async function save() {
    if (!policy || busy.current) return;
    const workspaceBinding = policyWorkspace.current;
    if (!workspaceBinding || currentWorkspaceBinding() !== workspaceBinding) {
      setPolicy(null);
      setError("The database session changed. Refresh cleanup settings.");
      return;
    }
    busy.current = true;
    setSaving(true);
    setError(null);
    setMessage(null);
    const request = guard.begin("policy");
    try {
      const data = policyResponse(
        await apiPut("/api/settings/console-cleanup", policy, { signal: request.signal, timeoutMs: 4000, workspaceBinding }),
      );
      if (request.isCurrent()) {
        setPolicy(data);
        setMessage(
          data.enabled ? "Automatic cleanup saved. Next check is at the start of the next hour." : "Automatic console cleanup disabled.",
        );
      }
    } catch (error) {
      if (request.isCurrent()) {
        setPolicy(null);
        setError(
          `${errorMessage(error, "Could not verify cleanup settings.")} Refresh before saving again; the update may already have completed.`,
        );
      }
    } finally {
      busy.current = false;
      if (request.isCurrent()) setSaving(false);
      request.complete();
    }
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-3">
        <CardTitle>Automatic console cleanup</CardTitle>
        <Button
          type="button"
          variant="outline"
          className="h-9 w-9 px-0"
          disabled={saving}
          title="Refresh console cleanup"
          aria-label="Refresh console cleanup"
          onClick={() => void load()}
        >
          <RefreshCw className="h-4 w-4" />
        </Button>
      </CardHeader>
      <CardContent className="grid gap-4">
        <label className="flex items-center gap-2 text-sm">
          <input
            type="checkbox"
            checked={policy?.enabled ?? false}
            disabled={!policy || saving}
            onChange={(event) => setPolicy((current) => current && { ...current, enabled: event.target.checked })}
          />
          Automatic idle cleanup
        </label>
        <Field>
          Idle threshold
          <Select
            value={policy?.idle_minutes ?? 60}
            disabled={!policy || !policy.enabled || saving}
            onChange={(event) =>
              setPolicy((current) => current && { ...current, idle_minutes: Number(event.target.value) as Policy["idle_minutes"] })
            }
          >
            {idleOptions.map((minutes) => (
              <option key={minutes} value={minutes}>
                {idleLabel(minutes)}
              </option>
            ))}
          </Select>
        </Field>
        {policy?.enabled ? (
          <Notice tone="warn">
            At each hour while this database is unlocked, eligible idle consoles may be closed, including agent consoles. Viewers will
            disconnect and detached remote work may be affected. Running or uncertain work is protected; history is retained.
          </Notice>
        ) : null}
        {!policy && !error ? <Notice>Loading console cleanup settings...</Notice> : null}
        {error ? <Notice tone="bad">{error}</Notice> : null}
        {message ? <Notice tone="good">{message}</Notice> : null}
        <Button type="button" variant="outline" disabled={!policy || saving} onClick={() => void save()}>
          <Save className="h-4 w-4" />
          {saving ? "Saving..." : "Save console cleanup"}
        </Button>
      </CardContent>
    </Card>
  );
}
