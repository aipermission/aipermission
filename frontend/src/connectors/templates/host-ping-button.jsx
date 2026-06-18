import { Activity, CheckCircle2, Loader2, XCircle } from "lucide-react";
import { useState } from "react";
import { Badge } from "../../components/ui/badge";
import { Button } from "../../components/ui/button";
import { Dialog } from "../../components/ui/dialog";
import { Notice } from "../../components/ui/notice";
import { apiPost } from "../../lib/api";

export function HostPingButton({ host, port, mode = "direct", transportTargetRef = "", label = "Ping host" }) {
  const [dialog, setDialog] = useState({ open: false, state: "idle", result: null, error: "" });
  const normalizedMode = mode || "direct";
  const numericPort = Number(port);
  const disabledReason = pingDisabledReason({ host, port: numericPort, mode: normalizedMode, transportTargetRef });

  async function runPing(event) {
    event.preventDefault();
    event.stopPropagation();
    if (disabledReason) return;
    setDialog({ open: true, state: "running", result: null, error: "" });
    try {
      const result = await apiPost("/api/connector-targets/ping", {
        host,
        port: numericPort,
        mode: normalizedMode,
        transport_target_ref: transportTargetRef || "",
        attempts: 4,
      });
      setDialog({ open: true, state: "done", result, error: "" });
    } catch (error) {
      setDialog({ open: true, state: "error", result: null, error: error.message || "Ping failed." });
    }
  }

  function closeDialog() {
    setDialog((current) => ({ ...current, open: false }));
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        className="h-8 w-8 shrink-0 px-0"
        title={disabledReason || label}
        aria-label={label}
        disabled={Boolean(disabledReason)}
        onClick={runPing}
      >
        <Activity className="h-4 w-4" />
      </Button>
      <Dialog
        open={dialog.open}
        title="Host reachability"
        description={`${modeLabel(normalizedMode)} TCP check for ${host || "host"}:${numericPort || "port"}`}
        onClose={closeDialog}
        closeOnOverlay={false}
        size="md"
      >
        <div className="grid gap-4">
          {dialog.state === "running" ? (
            <Notice>
              <span className="inline-flex items-center gap-2">
                <Loader2 className="h-4 w-4 animate-spin" />
                Running 4 reachability checks...
              </span>
            </Notice>
          ) : null}
          {dialog.error ? <Notice tone="bad">{dialog.error}</Notice> : null}
          {dialog.result ? (
            <>
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={dialog.result.ok ? "good" : dialog.result.received > 0 ? "warn" : "bad"}>{dialog.result.received}/{dialog.result.sent} reachable</Badge>
                <Badge tone="neutral">{dialog.result.duration_ms} ms total</Badge>
                <Badge tone="neutral">{modeLabel(dialog.result.mode)}</Badge>
              </div>
              <Notice tone={dialog.result.ok ? "good" : "warn"}>{dialog.result.message}</Notice>
              <div className="overflow-hidden rounded-lg border border-stone-200">
                {(dialog.result.attempts || []).map((attempt) => (
                  <div key={attempt.attempt} className="grid gap-2 border-b border-stone-200 px-3 py-2 last:border-b-0 sm:grid-cols-[92px_120px_minmax(0,1fr)]">
                    <span className="text-sm font-semibold text-stone-900">Attempt {attempt.attempt}</span>
                    <span className={`inline-flex items-center gap-1.5 text-sm font-semibold ${attempt.ok ? "text-emerald-700" : "text-red-700"}`}>
                      {attempt.ok ? <CheckCircle2 className="h-4 w-4" /> : <XCircle className="h-4 w-4" />}
                      {attempt.ok ? "reachable" : "failed"}
                    </span>
                    <span className="min-w-0 truncate font-mono text-xs text-stone-500" title={attempt.error || `${attempt.duration_ms} ms`}>
                      {attempt.error || `${attempt.duration_ms} ms`}
                    </span>
                  </div>
                ))}
              </div>
            </>
          ) : null}
          <div className="flex justify-end">
            <Button type="button" variant="outline" onClick={closeDialog}>
              Close
            </Button>
          </div>
        </div>
      </Dialog>
    </>
  );
}

function pingDisabledReason({ host, port, mode, transportTargetRef }) {
  if (!String(host || "").trim()) return "Enter a host first.";
  if (!Number.isInteger(port) || port < 1 || port > 65535) return "Enter a valid port first.";
  if (mode === "over_ssh" && !String(transportTargetRef || "").trim()) return "Select an SSH transport profile first.";
  return "";
}

function modeLabel(mode) {
  return mode === "over_ssh" ? "Over SSH" : "Direct";
}
