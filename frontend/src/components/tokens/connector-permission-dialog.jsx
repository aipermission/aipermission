import { useEffect, useMemo, useState } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { Button } from "../ui/button";
import { Dialog } from "../ui/dialog";
import { Badge } from "../ui/badge";
import { Notice } from "../ui/notice";
import { ConnectorRuleButton } from "../connectors/connector-rule-button";

const emptyLoad = { state: "idle", catalog: [], targets: [], actionsByProfile: {}, permissions: [], error: null };

export function ConnectorPermissionDialog({ token, onClose, onSaved }) {
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
        for (const action of load.actionsByProfile[profileActionKey(target.id, profile.id)] || []) {
          items.push({ target, profile, action, key: permissionKey(target.id, profile.id, action.name) });
        }
      }
    }
    return items;
  }, [load.targets, load.actionsByProfile]);

  async function loadConnectorPermissions(tokenID) {
    setLoad((current) => ({ ...current, state: "loading", error: null }));
    try {
      const [catalog, targetList, permissions] = await Promise.all([
        apiGet("/api/connectors"),
        apiGet("/api/connector-targets/inventory"),
        apiGet(`/api/tokens/${tokenID}/connector-permissions`),
      ]);
      const targets = targetList.items || [];
      const actionEntries = targets.flatMap((target) =>
        (target.profiles || []).map((profile) => [profileActionKey(target.id, profile.id), profile.actions || []])
      );
      const actionsByProfile = Object.fromEntries(actionEntries);
      const permissionItems = permissions.items || [];
      setLoad({ state: "ready", catalog: catalog.items || [], targets, actionsByProfile, permissions: permissionItems, error: null });
      setDraft(
        Object.fromEntries(
          permissionItems
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
      setLoad({ state: "error", catalog: [], targets: [], actionsByProfile: {}, permissions: [], error: error.message });
      setDraft({});
    }
  }

  function setRule(key, rule) {
    setDraft((current) => ({
      ...current,
      [key]: rule ? { execution_rule: rule, expires_at: rule === "blocked" ? "" : current[key]?.expires_at || "" } : { execution_rule: "", expires_at: "" },
    }));
  }

  async function savePermissions(event) {
    event.preventDefault();
    if (!token) return;
    setSave({ state: "saving", error: null });
    try {
      const knownKeys = new Set(rows.map((row) => row.key));
      const preserved = load.permissions
        .filter((permission) => !knownKeys.has(permissionKey(permission.target_id, permission.profile_id, permission.action_name)))
        .map((permission) => ({
          target_id: permission.target_id,
          profile_id: permission.profile_id,
          action_name: permission.action_name,
          execution_rule: permission.execution_rule,
          expires_at: permission.execution_rule === "blocked" ? undefined : permission.expires_at || undefined,
        }));
      const connectorPermissions = rows
        .map((row) => {
          const permission = draft[row.key];
          if (!permission?.execution_rule) return null;
          return {
            target_id: row.target.id,
            profile_id: row.profile.id,
            action_name: row.action.name,
            execution_rule: permission.execution_rule,
            expires_at: permission.execution_rule === "blocked" ? undefined : permission.expires_at || undefined,
          };
        })
        .filter(Boolean);
      const result = await apiPut(`/api/tokens/${token.id}/connector-permissions`, { permissions: [...preserved, ...connectorPermissions] });
      setLoad((current) => ({ ...current, permissions: result.items || [] }));
      await onSaved?.();
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
      description="Grant this token access to connector capabilities."
      onClose={onClose}
      size="xl"
      bodyClassName="max-h-[calc(100vh-180px)] overflow-y-auto"
    >
      <form className="grid gap-4" onSubmit={savePermissions}>
        <Notice>
          Security note: each connector grant binds one target, one credential profile, and one action. Prefer Prompt until you trust the workflow.
        </Notice>
        {load.state === "loading" ? <Notice>Loading connector targets...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {save.state === "error" ? <Notice tone="bad">{save.error}</Notice> : null}
        {save.state === "ready" ? <Notice tone="good">Connector permissions saved.</Notice> : null}
        {load.state === "ready" && rows.length === 0 ? <Notice>Create a connector target before granting action permissions.</Notice> : null}

        {load.state === "ready" && rows.length > 0 ? (
          <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
            <div className="grid grid-cols-[minmax(0,1fr)_230px] border-b border-stone-200 bg-stone-50 px-3 py-2 text-xs font-semibold uppercase text-stone-500">
              <span>Connector / target / profile / action</span>
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
                        <Badge tone="neutral">{connectorLabel(load.catalog, row.target.connector_kind)}</Badge>
                        <Badge tone="neutral">{row.profile.label}</Badge>
                        <Badge tone={row.action.risk === "read" ? "good" : "warn"}>{row.action.risk}</Badge>
                      </div>
                      <span className="truncate font-mono text-xs text-stone-700">{row.action.name}</span>
                      <span className="line-clamp-2 text-xs text-stone-500">{row.action.description}</span>
                    </div>
                    <div className="grid grid-cols-4 gap-1 self-start">
                      <ConnectorRuleButton active={!rule} onClick={() => setRule(row.key, "")}>
                        Disabled
                      </ConnectorRuleButton>
                      <ConnectorRuleButton active={rule === "blocked"} onClick={() => setRule(row.key, "blocked")}>
                        Blocked
                      </ConnectorRuleButton>
                      <ConnectorRuleButton active={rule === "approval_required"} onClick={() => setRule(row.key, "approval_required")}>
                        Prompt
                      </ConnectorRuleButton>
                      <ConnectorRuleButton active={rule === "always_run"} onClick={() => setRule(row.key, "always_run")}>
                        Always
                      </ConnectorRuleButton>
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

function permissionKey(targetID, profileID, actionName) {
  return `${targetID}:${profileID}:${actionName}`;
}

function profileActionKey(targetID, profileID) {
  return `${targetID}:${profileID}`;
}

function connectorLabel(catalog, kind) {
  return catalog.find((item) => item.kind === kind)?.label || kind;
}
