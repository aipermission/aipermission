import { KeyRound, PanelRightClose, PanelRightOpen, RefreshCcw, TicketCheck } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { apiGet, apiPut } from "../../lib/api";
import { maskedToken, ruleLabel } from "../../lib/permissions";
import { Badge, CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";

const emptyLoad = {
  state: "idle",
  actions: [],
  permissionsByToken: {},
  error: null,
};

export function ConnectorTokenPermissionPanel({ tokens, selectedTarget, compact = false, onToggleCompact, onRefresh }) {
  const activeTokens = tokens.data.filter((token) => !token.revoked_at);
  const [load, setLoad] = useState(emptyLoad);
  const [savingKey, setSavingKey] = useState("");
  const [openTokenID, setOpenTokenID] = useState(null);
  const compactPanelRef = useRef(null);
  const tokenIDsKey = activeTokens.map((token) => token.id).join(",");

  useEffect(() => {
    if (!selectedTarget) {
      setLoad(emptyLoad);
      return;
    }
    void loadConnectorPermissions();
  }, [selectedTarget?.ref, tokenIDsKey]);

  useEffect(() => {
    if (!openTokenID) return undefined;

    function closeOnOutsidePointer(event) {
      if (!compactPanelRef.current?.contains(event.target)) {
        setOpenTokenID(null);
      }
    }

    function closeOnEscape(event) {
      if (event.key === "Escape") {
        setOpenTokenID(null);
      }
    }

    window.addEventListener("pointerdown", closeOnOutsidePointer);
    window.addEventListener("keydown", closeOnEscape);
    return () => {
      window.removeEventListener("pointerdown", closeOnOutsidePointer);
      window.removeEventListener("keydown", closeOnEscape);
    };
  }, [openTokenID]);

  const selectedCountByToken = useMemo(() => {
    const result = {};
    for (const token of activeTokens) {
      result[token.id] = currentTargetPermissions(load.permissionsByToken[token.id] || [], selectedTarget).length;
    }
    return result;
  }, [activeTokens, load.permissionsByToken, selectedTarget]);

  async function loadConnectorPermissions() {
    if (!selectedTarget) return;
    setLoad((current) => ({ ...current, state: "loading", error: null }));
    try {
      const [connector, ...permissionLists] = await Promise.all([
        apiGet(`/api/connectors/${selectedTarget.connector_kind}`),
        ...activeTokens.map((token) => apiGet(`/api/tokens/${token.id}/connector-permissions`)),
      ]);
      const permissionsByToken = {};
      activeTokens.forEach((token, index) => {
        permissionsByToken[token.id] = permissionLists[index]?.items || [];
      });
      setLoad({ state: "ready", actions: connector.actions || [], permissionsByToken, error: null });
    } catch (error) {
      setLoad({ state: "error", actions: [], permissionsByToken: {}, error: error.message });
    }
  }

  async function refreshPanel() {
    await onRefresh?.();
    await loadConnectorPermissions();
  }

  async function setConnectorRule(token, action, rule) {
    if (!selectedTarget) return;
    const key = `${token.id}:${action.name}`;
    setSavingKey(key);
    setLoad((current) => ({ ...current, error: null }));
    try {
      const existing = load.permissionsByToken[token.id] || [];
      const preserved = existing.filter((permission) => !matchesTargetAction(permission, selectedTarget, action.name));
      const next = rule
        ? [
            ...preserved,
            {
              target_id: selectedTarget.target_id,
              profile_id: selectedTarget.profile_id,
              action_name: action.name,
              execution_rule: rule,
            },
          ]
        : preserved;
      const result = await apiPut(`/api/tokens/${token.id}/connector-permissions`, {
        permissions: next.map(permissionInput),
      });
      setLoad((current) => ({
        ...current,
        state: "ready",
        permissionsByToken: {
          ...current.permissionsByToken,
          [token.id]: result.items || [],
        },
        error: null,
      }));
    } catch (error) {
      setLoad((current) => ({ ...current, state: "error", error: error.message }));
    } finally {
      setSavingKey("");
    }
  }

  function renderTokenActions(token, compactPopover = false) {
    const permissions = load.permissionsByToken[token.id] || [];
    return (
      <div className="grid gap-2">
        {load.actions.map((action) => {
          const permission = permissions.find((item) => matchesTargetAction(item, selectedTarget, action.name));
          const rule = permission?.execution_rule || "";
          const key = `${token.id}:${action.name}`;
          return (
            <div key={action.name} className={`grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 ${compactPopover ? "" : "dark-panel-subtle"}`}>
              <div className="flex min-w-0 items-center justify-between gap-2">
                <div className="min-w-0">
                  <p className="truncate font-mono text-xs font-semibold text-stone-900">{action.name}</p>
                  <p className="line-clamp-2 text-xs text-stone-500">{action.description}</p>
                </div>
                <Badge tone={action.risk === "read" ? "good" : "warn"}>{action.risk}</Badge>
              </div>
              <div className="grid grid-cols-3 gap-1">
                <ConnectorRuleButton active={!rule} disabled={savingKey === key} onClick={() => setConnectorRule(token, action, "")}>
                  Disabled
                </ConnectorRuleButton>
                <ConnectorRuleButton active={rule === "approval_required"} disabled={savingKey === key} onClick={() => setConnectorRule(token, action, "approval_required")}>
                  Prompt
                </ConnectorRuleButton>
                <ConnectorRuleButton active={rule === "always_run"} disabled={savingKey === key} onClick={() => setConnectorRule(token, action, "always_run")}>
                  Always
                </ConnectorRuleButton>
              </div>
            </div>
          );
        })}
        {load.state === "ready" && load.actions.length === 0 ? <Notice>No actions exposed by this connector.</Notice> : null}
      </div>
    );
  }

  if (compact) {
    return (
      <aside ref={compactPanelRef} className="relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-visible rounded-lg border border-stone-200 bg-white">
        <header className="grid gap-2 border-b border-stone-200 p-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand tokens" onClick={onToggleCompact}>
            <PanelRightOpen className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh connector permissions" onClick={refreshPanel}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </header>
        <div className="grid content-start gap-2 overflow-visible p-2">
          {activeTokens.map((token) => {
            const open = Number(openTokenID) === Number(token.id);
            const selectedCount = selectedCountByToken[token.id] || 0;
            return (
              <div className="relative" key={token.id}>
                <button
                  type="button"
                  className={`relative grid h-10 w-10 place-items-center rounded-md border text-stone-700 transition hover:bg-stone-100 ${selectedCount > 0 ? "border-emerald-700" : "border-stone-300"}`}
                  title={`${token.name}: ${selectedCount} connector grants`}
                  onClick={() => setOpenTokenID(open ? null : token.id)}
                >
                  <KeyRound className="h-4 w-4" />
                  {selectedCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{selectedCount}</CountBadge> : null}
                </button>
                {open ? (
                  <div className="absolute right-full top-0 z-30 mr-2 grid max-h-[70vh] w-96 gap-3 overflow-auto rounded-lg border border-stone-200 bg-white p-3 shadow-xl">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-stone-900">{token.name}</p>
                      <p className="mt-1 text-xs text-stone-500">
                        {selectedTarget.target_name} / {selectedTarget.profile_label}
                      </p>
                    </div>
                    {renderTokenActions(token, true)}
                  </div>
                ) : null}
              </div>
            );
          })}
        </div>
      </aside>
    );
  }

  return (
    <aside className="grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-hidden rounded-lg border border-stone-200 bg-white">
      <header className="flex items-center justify-between gap-3 border-b border-stone-200 px-4 py-3">
        <div className="min-w-0">
          <h3 className="flex items-center gap-2 text-sm font-semibold">
            <TicketCheck className="h-4 w-4" />
            Tokens
          </h3>
          <p className="mt-1 truncate text-xs text-stone-500">
            {selectedTarget.target_name} / {selectedTarget.profile_label}
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse tokens" onClick={onToggleCompact}>
            <PanelRightClose className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh connector permissions" onClick={refreshPanel} disabled={load.state === "loading"}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <div className="min-h-0 overflow-auto p-3">
        <Notice>
          Connector permissions bind one token, target profile, and action. Prefer Prompt until you trust the workflow.
        </Notice>
        {load.state === "loading" ? <Notice>Loading connector permissions...</Notice> : null}
        {load.state === "error" ? <Notice tone="bad">{load.error}</Notice> : null}
        {tokens.state === "error" ? <Notice tone="bad">{tokens.error}</Notice> : null}
        {tokens.state === "ready" && tokens.data.length === 0 ? <Notice>Create a token first.</Notice> : null}
        {tokens.state === "ready" && tokens.data.length > 0 && activeTokens.length === 0 ? <Notice>No active tokens.</Notice> : null}

        <div className="mt-3 grid gap-3">
          {activeTokens.map((token) => {
            const selectedCount = selectedCountByToken[token.id] || 0;
            return (
              <section className={`grid gap-3 rounded-lg border p-3 transition ${selectedCount > 0 ? "border-emerald-200 bg-emerald-50" : "border-stone-200 bg-white"}`} key={token.id}>
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="flex max-w-full min-w-0 items-center gap-2 text-sm font-semibold">
                      <KeyRound className="h-4 w-4 shrink-0 text-stone-500" />
                      <span className="truncate">{token.name}</span>
                    </p>
                    <p className="mt-1 truncate font-mono text-[11px] text-stone-500">{maskedToken(token.token)}</p>
                  </div>
                  <Badge tone={selectedCount > 0 ? "good" : "neutral"}>{selectedCount > 0 ? `${selectedCount} grants` : ruleLabel("")}</Badge>
                </div>
                {renderTokenActions(token)}
              </section>
            );
          })}
        </div>
      </div>
    </aside>
  );
}

function ConnectorRuleButton({ active, children, ...props }) {
  return (
    <button
      type="button"
      className={`h-8 rounded-md border px-2 text-xs font-semibold transition disabled:pointer-events-none disabled:opacity-50 ${
        active ? "permission-button-active border-emerald-900 bg-emerald-950 text-white" : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"
      }`}
      {...props}
    >
      {children}
    </button>
  );
}

function currentTargetPermissions(permissions, target) {
  if (!target) return [];
  return permissions.filter((permission) => Number(permission.target_id) === Number(target.target_id) && Number(permission.profile_id) === Number(target.profile_id));
}

function matchesTargetAction(permission, target, actionName) {
  return Number(permission.target_id) === Number(target.target_id) && Number(permission.profile_id) === Number(target.profile_id) && permission.action_name === actionName;
}

function permissionInput(permission) {
  return {
    target_id: permission.target_id,
    profile_id: permission.profile_id,
    action_name: permission.action_name,
    execution_rule: permission.execution_rule,
    expires_at: permission.expires_at || undefined,
  };
}
