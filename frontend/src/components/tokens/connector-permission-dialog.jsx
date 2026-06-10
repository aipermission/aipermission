import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Badge } from "../ui/badge";
import { Notice } from "../ui/notice";

const emptyLoad = { state: "idle", targets: [], actions: [], permissions: [], error: null };

export function ConnectorPermissionDialog({ token, onClose }) {
  const [load, setLoad] = useState(emptyLoad);
  const [draft, setDraft] = useState({});
  const [save, setSave] = useState({ state: "idle", error: null });

  useEffect(() => {
    if (!token) {
      setLoad(emptyLoad);
      setDraft({});
      setSave({ state: "idle", error: null });
      return;
    }
    void loadConnectorPermissions(token.id);
  }, [token?.id]);

  const rows = useMemo(() => {
    const items = [];
    for (const target of load.targets) {
      for (const profile of target.profiles || []) {
        for (const action of load.actions) {
          items.push({ target, profile, action, key: permissionKey(target.id, profile.id, action.name) });
        }
      }
    }
    return items;
  }, [load.targets, load.actions]);

  async function loadConnectorPermissions(tokenID) {
    setLoad((current) => ({ ...current, state: "loading", error: null }));
    try {
      const [connector, targetList, permissions] = await Promise.all([
        apiGet("/api/connectors/postgres"),
        apiGet("/api/connector-targets?kind=postgres"),
        apiGet(`/api/tokens/${tokenID}/connector-permissions`),
      ]);
      const targets = await Promise.all((targetList.items || []).map((target) => apiGet(`/api/connector-targets/${target.id}`)));
      const permissionItems = permissions.items || [];
      setLoad({ state: "ready", targets, actions: connector.actions || [], permissions: permissionItems, error: null });
      setDraft(
        Object.fromEntries(
          permissionItems
            .filter((permission) => permission.connector_kind === "postgres")
            .map((permission) => [
              permissionKey(permission.target_id, permission.profile_id, permission.action_name),
              {
                execution_rule: permission.execution_rule,
                expires_at: permission.expires_at || "",
              },
            ])
        )
      );
    } catch (error) {
      setLoad({ state: "error", targets: [], actions: [], permissions: [], error: error.message });
      setDraft({});
    }
  }

  function setRule(key, rule) {
    setDraft((current) => ({
      ...current,
      [key]: rule ? { execution_rule: rule, expires_at: current[key]?.expires_at || "" } : { execution_rule: "", expires_at: "" },
    }));
  }

  async function savePermissions(event) {
    event.preventDefault();
    if (!token) return;
    setSave({ state: "saving", error: null });
    try {
      const preserved = load.permissions
        .filter((permission) => permission.connector_kind !== "postgres")
        .map((permission) => ({
          target_id: permission.target_id,
          profile_id: permission.profile_id,
          action_name: permission.action_name,
          execution_rule: permission.execution_rule,
          expires_at: permission.expires_at || undefined,
        }));
      const postgresPermissions = rows
        .map((row) => {
          const permission = draft[row.key];
          if (!permission?.execution_rule) return null;
          return {
            target_id: row.target.id,
            profile_id: row.profile.id,
            action_name: row.action.name,
            execution_rule: permission.execution_rule,
            expires_at: permission.expires_at || undefined,
          };
        })
        .filter(Boolean);
      const result = await apiPut(`/api/tokens/${token.id}/connector-permissions`, { permissions: [...preserved, ...postgresPermissions] });
      setLoad((current) => ({ ...current, permissions: result.items || [] }));
      setSave({ state: "ready", error: null });
    } catch (error) {
      setSave({ state: "error", error: error.message });
    }
  }

  const selectedCount = Object.values(draft).filter((permission) => Boolean(permission?.execution_rule)).length;

  return (
    <Dialog
      open={Boolean(token)}
      title={token ? `${token.name} connector permissions` : "Connector permissions"}
      description="Grant this token access to connector actions."
      onClose={onClose}
      size="xl"
      bodyClassName="max-h-[calc(100vh-180px)] overflow-y-auto"
    >
      <form className="grid gap-4" onSubmit={savePermissions}>
        <Notice>
          Security note: each Postgres grant binds one target, one credential profile, and one action. Prefer Prompt until you trust the workflow.
        </Notice>
        {load.state === "loading" ? <Notice>Loading connector targets...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {save.state === "error" ? <Notice tone="bad">{save.error}</Notice> : null}
        {save.state === "ready" ? <Notice tone="good">Connector permissions saved.</Notice> : null}
        {load.state === "ready" && rows.length === 0 ? <Notice>Create a Postgres connector target before granting connector permissions.</Notice> : null}

        {load.state === "ready" && rows.length > 0 ? (
          <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
            <div className="grid grid-cols-[minmax(0,1fr)_230px] border-b border-stone-200 bg-stone-50 px-3 py-2 text-xs font-semibold uppercase text-stone-500">
              <span>Target / profile / action</span>
              <span>Rule</span>
            </div>
            <div className="max-h-[440px] divide-y divide-stone-200 overflow-y-auto">
              {rows.map((row) => {
                const rule = draft[row.key]?.execution_rule || "";
                return (
                  <div key={row.key} className="grid gap-3 px-3 py-3 md:grid-cols-[minmax(0,1fr)_230px]">
                    <div className="grid min-w-0 gap-1">
                      <div className="flex min-w-0 flex-wrap items-center gap-2">
                        <span className="truncate font-semibold text-stone-950">{row.target.name}</span>
                        <Badge tone="neutral">{row.profile.label}</Badge>
                        <Badge tone={row.action.risk === "read" ? "good" : "warn"}>{row.action.risk}</Badge>
                      </div>
                      <span className="truncate font-mono text-xs text-stone-700">{row.action.name}</span>
                      <span className="line-clamp-2 text-xs text-stone-500">{row.action.description}</span>
                    </div>
                    <div className="grid grid-cols-3 gap-1 self-start">
                      <RuleButton active={!rule} onClick={() => setRule(row.key, "")}>
                        Disabled
                      </RuleButton>
                      <RuleButton active={rule === "approval_required"} onClick={() => setRule(row.key, "approval_required")}>
                        Prompt
                      </RuleButton>
                      <RuleButton active={rule === "always_run"} onClick={() => setRule(row.key, "always_run")}>
                        Always
                      </RuleButton>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>
        ) : null}

        <div className="flex flex-wrap items-center justify-between gap-3">
          <p className="text-sm text-stone-500">{selectedCount} connector action grant{selectedCount === 1 ? "" : "s"} selected.</p>
          <div className="flex gap-2">
            <Button type="button" variant="outline" onClick={onClose}>
              Close
            </Button>
            <Button type="submit" disabled={!token || load.state !== "ready" || save.state === "saving"}>
              {save.state === "saving" ? "Saving..." : "Save connector permissions"}
            </Button>
          </div>
        </div>
      </form>
    </Dialog>
  );
}

function RuleButton({ active, className = "", ...props }) {
  return (
    <button
      type="button"
      className={`h-9 rounded-md border px-2 text-xs font-semibold transition ${
        active
          ? "border-emerald-800 bg-emerald-950 text-white permission-button-active"
          : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"
      } ${className}`}
      {...props}
    />
  );
}

function permissionKey(targetID, profileID, actionName) {
  return `${targetID}:${profileID}:${actionName}`;
}
