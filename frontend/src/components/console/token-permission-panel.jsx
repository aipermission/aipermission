import { KeyRound, PanelRightClose, PanelRightOpen, RefreshCcw, TicketCheck } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { effectiveRule, expiresAtFromLifetime, maskedToken, permissionCardClass, permissionLifetimeLabel, ruleLabel } from "../../lib/permissions";
import { Badge, CountBadge } from "../ui/badge";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";

export function TokenPermissionPanel({ tokens, selectedServer, permissionState, unreadMessages, compact = false, onToggleCompact, onRefresh, onSetRule, onOpenMessages }) {
  const activeTokens = tokens.data.filter((token) => !token.revoked_at);
  const [openTokenID, setOpenTokenID] = useState(null);
  const compactPanelRef = useRef(null);

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

  if (compact) {
    return (
      <aside ref={compactPanelRef} className="relative grid min-h-0 grid-rows-[auto_minmax(0,1fr)] overflow-visible rounded-lg border border-stone-200 bg-white">
        <header className="grid gap-2 border-b border-stone-200 p-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Expand tokens" onClick={onToggleCompact}>
            <PanelRightOpen className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh token permissions" onClick={onRefresh}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </header>
        <div className="grid content-start gap-2 overflow-visible p-2">
          {activeTokens.map((token) => {
            const permissions = permissionState.data[token.id] || {};
            const selectedPermission = selectedServer ? permissions[selectedServer.id] : null;
            const selectedRule = effectiveRule(selectedPermission);
            const unreadCount = selectedServer
              ? unreadMessages.filter((message) => Number(message.server_id) === Number(selectedServer.id) && Number(message.token_id) === Number(token.id)).length
              : unreadMessages.filter((message) => Number(message.token_id) === Number(token.id)).length;
            const open = Number(openTokenID) === Number(token.id);
            return (
              <div className="relative" key={token.id}>
                <button
                  type="button"
                  className={`relative grid h-10 w-10 place-items-center rounded-md border text-stone-700 transition hover:bg-stone-100 ${selectedRule === "always_run" ? "border-emerald-700" : selectedRule === "approval_required" ? "border-amber-500" : "border-stone-300"}`}
                  title={`${token.name}: ${ruleLabel(selectedRule)}`}
                  onClick={() => setOpenTokenID(open ? null : token.id)}
                >
                  <KeyRound className="h-4 w-4" />
                  {unreadCount > 0 ? <CountBadge className="absolute -right-1 -top-1">{unreadCount}</CountBadge> : null}
                </button>
                {open && selectedServer ? (
                  <div className="absolute right-full top-0 z-30 mr-2 grid w-80 gap-3 rounded-lg border border-stone-200 bg-white p-3 shadow-xl">
                    <div className="min-w-0">
                      <p className="truncate text-sm font-semibold text-stone-900">{token.name}</p>
                      <p className="mt-1 text-xs text-stone-500">
                        {ruleLabel(selectedRule)} on {selectedServer.name}
                        {selectedRule ? ` - ${permissionLifetimeLabel(selectedPermission)}` : ""}
                      </p>
                    </div>
                    <PermissionRuleButtons
                      saving={permissionState.savingKey === `${token.id}:${selectedServer.id}`}
                      selectedRule={selectedRule}
                      onDisable={() => onSetRule(token, selectedServer, "")}
                      onPrompt={() => onSetRule(token, selectedServer, "approval_required")}
                      onAlways={() => onSetRule(token, selectedServer, "always_run")}
                    />
                    {unreadCount > 0 ? (
                      <Button type="button" variant="outline" className="h-8 px-2 text-xs" onClick={() => onOpenMessages(token.id)}>
                        Messages
                      </Button>
                    ) : null}
                    {selectedRule ? (
                      <PermissionLifetimeControls
                        value={selectedPermission}
                        onSetPermanent={() => onSetRule(token, selectedServer, selectedRule, { expiresAt: "" })}
                        onSetTemporary={(lifetime) => onSetRule(token, selectedServer, selectedRule, { expiresAt: expiresAtFromLifetime(lifetime) })}
                      />
                    ) : null}
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
            {selectedServer ? `Current target: ${selectedServer.name}` : "Select a server"}
          </p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="ghost" className="h-9 w-9 px-0" title="Collapse tokens" onClick={onToggleCompact}>
            <PanelRightClose className="h-4 w-4" />
          </Button>
          <Button type="button" variant="outline" className="h-9 w-9 px-0" title="Refresh token permissions" onClick={onRefresh}>
            <RefreshCcw className="h-4 w-4" />
          </Button>
        </div>
      </header>

      <div className="min-h-0 overflow-auto p-3">
        {permissionState.state === "error" ? <Notice tone="bad">{permissionState.error}</Notice> : null}
        {tokens.state === "error" ? <Notice tone="bad">{tokens.error}</Notice> : null}
        {tokens.state === "ready" && tokens.data.length === 0 ? <Notice>Create a token first.</Notice> : null}
        {tokens.state === "ready" && tokens.data.length > 0 && activeTokens.length === 0 ? <Notice>No active tokens.</Notice> : null}

        <div className="grid gap-3">
          {activeTokens.map((token) => {
            const permissions = permissionState.data[token.id] || {};
            const selectedPermission = selectedServer ? permissions[selectedServer.id] : null;
            const selectedRule = effectiveRule(selectedPermission);
            const unreadCount = selectedServer
              ? unreadMessages.filter((message) => Number(message.server_id) === Number(selectedServer.id) && Number(message.token_id) === Number(token.id)).length
              : unreadMessages.filter((message) => Number(message.token_id) === Number(token.id)).length;
            return (
              <section className={`grid gap-3 rounded-lg border p-3 transition ${permissionCardClass(selectedRule)}`} key={token.id}>
                <div className="flex min-w-0 items-start justify-between gap-3">
                  <div className="min-w-0">
                    <button
                      type="button"
                      className={`flex max-w-full min-w-0 items-center gap-2 text-left text-sm font-semibold ${
                        unreadCount > 0 ? "cursor-pointer hover:text-emerald-700" : "cursor-default"
                      }`}
                      onClick={() => unreadCount > 0 && onOpenMessages(token.id)}
                    >
                      <KeyRound className="h-4 w-4 shrink-0 text-stone-500" />
                      <span className="truncate">{token.name}</span>
                      {unreadCount > 0 ? <CountBadge>{unreadCount}</CountBadge> : null}
                    </button>
                    <p className="mt-1 truncate font-mono text-[11px] text-stone-500">{maskedToken(token.token)}</p>
                  </div>
                  <Badge tone="good">active</Badge>
                </div>

                {selectedServer ? (
                  <>
                    <PermissionRuleButtons
                      saving={permissionState.savingKey === `${token.id}:${selectedServer.id}`}
                      selectedRule={selectedRule}
                      onDisable={() => onSetRule(token, selectedServer, "")}
                      onPrompt={() => onSetRule(token, selectedServer, "approval_required")}
                      onAlways={() => onSetRule(token, selectedServer, "always_run")}
                    />
                    {selectedRule ? (
                      <PermissionLifetimeControls
                        value={selectedPermission}
                        onSetPermanent={() => onSetRule(token, selectedServer, selectedRule, { expiresAt: "" })}
                        onSetTemporary={(lifetime) => onSetRule(token, selectedServer, selectedRule, { expiresAt: expiresAtFromLifetime(lifetime) })}
                      />
                    ) : null}
                  </>
                ) : null}
              </section>
            );
          })}
        </div>
      </div>
    </aside>
  );
}

function PermissionRuleButtons({ saving, selectedRule, onDisable, onPrompt, onAlways }) {
  return (
    <div className="grid grid-cols-3 gap-1">
      <PermissionButton active={!selectedRule} disabled={saving} onClick={onDisable}>
        Disabled
      </PermissionButton>
      <PermissionButton active={selectedRule === "approval_required"} disabled={saving} onClick={onPrompt}>
        Prompt
      </PermissionButton>
      <PermissionButton active={selectedRule === "always_run"} disabled={saving} onClick={onAlways}>
        Always
      </PermissionButton>
    </div>
  );
}

function PermissionLifetimeControls({ value, onSetPermanent, onSetTemporary }) {
  return (
    <div className="dark-panel-subtle grid gap-2 rounded-md border border-stone-200 bg-white/70 p-2 text-xs">
      <div className="flex items-center justify-between gap-2">
        <span className="font-semibold text-stone-700">Lifetime</span>
        <span className="text-stone-500">{permissionLifetimeLabel(value)}</span>
      </div>
      <div className="grid grid-cols-4 gap-1">
        <PermissionButton active={!value?.expires_at} onClick={onSetPermanent}>
          Keep
        </PermissionButton>
        <PermissionButton active={false} onClick={() => onSetTemporary("1h")}>
          1h
        </PermissionButton>
        <PermissionButton active={false} onClick={() => onSetTemporary("4h")}>
          4h
        </PermissionButton>
        <PermissionButton active={false} onClick={() => onSetTemporary("1d")}>
          1d
        </PermissionButton>
      </div>
    </div>
  );
}

function PermissionButton({ active, children, ...props }) {
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
