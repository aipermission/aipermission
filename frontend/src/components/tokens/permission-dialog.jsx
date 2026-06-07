import { Dialog } from "../ui/dialog";
import { Button } from "../ui/button";
import { effectiveRule, expiresAtFromLifetime, permissionLifetimeLabel } from "../../lib/permissions";

export function PermissionDialog({ value, permissionState, onClose, onSetRule }) {
  const token = value?.token;
  const server = value?.server;
  const permission = token && server ? permissionState.data[token.id]?.[server.id] : null;
  const rule = effectiveRule(permission);
  const saving = token && server ? permissionState.savingKey === `${token.id}:${server.id}` : false;

  return (
    <Dialog
      open={Boolean(value)}
      title={token && server ? `${token.name} -> ${server.name}` : "Permission"}
      description="Choose how this token can use this server."
      onClose={onClose}
    >
      {token && server ? (
        <div className="grid gap-3">
          <PermissionChoice
            title="Always run"
            description="Commands can run without approval."
            active={rule === "always_run"}
            disabled={saving}
            onClick={() => onSetRule(token, server, "always_run")}
          />
          <PermissionChoice
            title="Prompt"
            description="Commands must wait for approval."
            active={rule === "approval_required"}
            disabled={saving}
            onClick={() => onSetRule(token, server, "approval_required")}
          />
          <PermissionChoice
            title="Disabled"
            description="This token cannot see or use this server."
            active={!rule}
            disabled={saving}
            onClick={() => onSetRule(token, server, "")}
          />
          {rule ? (
            <div className="dark-panel-subtle grid gap-2 rounded-lg border border-stone-200 bg-stone-50 p-3">
              <div className="flex items-center justify-between gap-2 text-sm">
                <span className="font-semibold text-stone-900">Lifetime</span>
                <span className="text-stone-500">{permissionLifetimeLabel(permission)}</span>
              </div>
              <div className="grid gap-2 sm:grid-cols-4">
                <Button type="button" variant="outline" className="h-9 px-2 text-xs" disabled={saving} onClick={() => onSetRule(token, server, rule, { expiresAt: "" })}>
                  Keep
                </Button>
                <Button type="button" variant="outline" className="h-9 px-2 text-xs" disabled={saving} onClick={() => onSetRule(token, server, rule, { expiresAt: expiresAtFromLifetime("1h") })}>
                  1 hour
                </Button>
                <Button type="button" variant="outline" className="h-9 px-2 text-xs" disabled={saving} onClick={() => onSetRule(token, server, rule, { expiresAt: expiresAtFromLifetime("4h") })}>
                  4 hours
                </Button>
                <Button type="button" variant="outline" className="h-9 px-2 text-xs" disabled={saving} onClick={() => onSetRule(token, server, rule, { expiresAt: expiresAtFromLifetime("1d") })}>
                  1 day
                </Button>
              </div>
            </div>
          ) : null}
        </div>
      ) : null}
    </Dialog>
  );
}

function PermissionChoice({ title, description, active, ...props }) {
  return (
    <button
      type="button"
      className={`rounded-lg border p-3 text-left transition disabled:pointer-events-none disabled:opacity-50 ${
        active ? "border-emerald-900 bg-emerald-50" : "border-stone-200 bg-white hover:bg-stone-50"
      }`}
      {...props}
    >
      <span className="block text-sm font-semibold text-stone-950">{title}</span>
      <span className="mt-1 block text-xs text-stone-500">{description}</span>
    </button>
  );
}
