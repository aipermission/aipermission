import { Ban, CalendarClock, Database, KeyRound, PlugZap, Plus, RefreshCcw, TicketCheck } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { apiPost } from "../lib/api";
import { useGateway } from "../lib/gateway-context";
import { effectiveRule, expiresAtFromLifetime, maskedToken, permissionLifetimeLabel, ruleDotClass, ruleLabel } from "../lib/permissions";
import { useAsyncAction } from "../lib/use-async-action";
import { useTokenPermissions } from "../lib/use-token-permissions";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import { CopyButton } from "../components/ui/copy-button";
import { Dialog } from "../components/ui/dialog";
import { Drawer } from "../components/ui/drawer";
import { Field, Input, Select } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { PermissionDialog } from "../components/tokens/permission-dialog";
import { ConnectorPermissionDialog } from "../components/tokens/connector-permission-dialog";
import { TokenInstallDialog } from "../components/tokens/token-install-dialog";

const tokenExpiryOptions = [
  { value: "never", label: "Never expires", ms: 0 },
  { value: "1h", label: "1 hour", ms: 60 * 60 * 1000 },
  { value: "4h", label: "4 hours", ms: 4 * 60 * 60 * 1000 },
  { value: "1d", label: "1 day", ms: 24 * 60 * 60 * 1000 },
  { value: "7d", label: "7 days", ms: 7 * 24 * 60 * 60 * 1000 },
];

const emptyForm = { name: "cursor-maintenance", expires_in: "never" };
export function TokensPage() {
  const { tokens, servers, loadTokens } = useGateway();
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [form, setForm] = useState(emptyForm);
  const [createdToken, setCreatedToken] = useState(null);
  const [permissionDialog, setPermissionDialog] = useState(null);
  const [connectorPermissionDialog, setConnectorPermissionDialog] = useState(null);
  const { permissionState, loadAllTokenPermissions, setTokenServerRule, setTokenAllServerRules } = useTokenPermissions(tokens.data);
  const [installDialog, setInstallDialog] = useState({ open: false, token: null, provider: "manual" });
  const [bulkDialog, setBulkDialog] = useState({ open: false, token: null, rule: "approval_required", lifetime: "permanent" });
  const { actionState: state, runAction } = useAsyncAction();
  const [tokenFilter, setTokenFilter] = useState("active");

  const stats = useMemo(() => {
    const active = tokens.data.filter((token) => tokenStatus(token) === "active").length;
    const expired = tokens.data.filter((token) => tokenStatus(token) === "expired").length;
    return {
      total: tokens.data.length,
      active,
      expired,
      revoked: tokens.data.filter((token) => Boolean(token.revoked_at)).length,
    };
  }, [tokens.data]);

  const visibleTokens = useMemo(() => {
    if (tokenFilter === "active") return tokens.data.filter((token) => tokenStatus(token) === "active");
    if (tokenFilter === "expired") return tokens.data.filter((token) => tokenStatus(token) === "expired");
    if (tokenFilter === "revoked") return tokens.data.filter((token) => Boolean(token.revoked_at));
    return tokens.data;
  }, [tokenFilter, tokens.data]);

  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadAllTokenPermissions(tokens.data);
  }, [tokens.state, tokens.data.map((token) => token.id).join(",")]);

  async function refreshTokensAndPermissions() {
    const tokenItems = await loadTokens();
    await loadAllTokenPermissions(tokenItems);
  }

  async function createToken(event) {
    event.preventDefault();
    await runAction({
      pending: "saving",
      successMessage: "Token created.",
      action: async () => {
        const token = await apiPost("/api/tokens", tokenCreatePayload(form));
        setCreatedToken(token);
        setForm(emptyForm);
        setDrawerOpen(false);
        await loadTokens();
      },
    });
  }

  async function revokeToken(token) {
    await runAction({
      pending: "revoking",
      successMessage: `${token.name} revoked.`,
      action: async () => {
        await apiPost(`/api/tokens/${token.id}/revoke`, {});
        await loadTokens();
      },
    });
  }

  async function applyBulkPermissions(event) {
    event.preventDefault();
    if (!bulkDialog.token) return;
    await setTokenAllServerRules(bulkDialog.token, servers.data, bulkDialog.rule, { expiresAt: expiresAtFromLifetime(bulkDialog.lifetime) });
    setBulkDialog({ open: false, token: null, rule: "approval_required", lifetime: "permanent" });
  }

  return (
    <section className="mx-auto grid w-full max-w-7xl gap-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">API tokens</h3>
          <p className="text-sm text-stone-500">Create revokable gateway tokens for MCP clients and AI tools.</p>
        </div>
        <div className="flex gap-2">
          <Button type="button" variant="outline" onClick={refreshTokensAndPermissions}>
            <RefreshCcw className="h-4 w-4" />
            Refresh
          </Button>
          <Button type="button" onClick={() => setDrawerOpen(true)}>
            <Plus className="h-4 w-4" />
            Add token
          </Button>
        </div>
      </div>

      <div className="grid gap-3 md:grid-cols-4">
        <TokenStat
          icon={TicketCheck}
          label="Total tokens"
          value={stats.total}
          selected={tokenFilter === "all"}
          onClick={() => setTokenFilter("all")}
        />
        <TokenStat
          icon={KeyRound}
          label="Active"
          value={stats.active}
          tone="good"
          selected={tokenFilter === "active"}
          onClick={() => setTokenFilter("active")}
        />
        <TokenStat
          icon={Ban}
          label="Expired"
          value={stats.expired}
          tone="warn"
          selected={tokenFilter === "expired"}
          onClick={() => setTokenFilter("expired")}
        />
        <TokenStat
          icon={Ban}
          label="Revoked"
          value={stats.revoked}
          tone="bad"
          selected={tokenFilter === "revoked"}
          onClick={() => setTokenFilter("revoked")}
        />
      </div>

      {createdToken ? (
        <Notice tone="good">
          <div className="grid gap-2">
            <strong>{createdToken.name} token created.</strong>
            <span className="text-sm">Copy it now. If reusable token copy is off in Settings, this value will not be shown again.</span>
            <div className="flex gap-2">
              <Input className="font-mono text-xs" readOnly value={maskedToken(createdToken.token)} />
              <CopyButton value={createdToken.token} variant="outline" />
            </div>
          </div>
        </Notice>
      ) : null}
      {state.message ? <Notice tone="good">{state.message}</Notice> : null}
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      {tokens.state === "error" ? <Notice tone="bad">{tokens.error}</Notice> : null}
      {permissionState.state === "error" ? <Notice tone="bad">{permissionState.error}</Notice> : null}

      <div className="overflow-hidden rounded-lg border border-stone-200 bg-white">
        <table className="w-full table-fixed border-collapse text-left text-sm">
          <thead className="bg-stone-50 text-xs uppercase text-stone-500">
            <tr>
              <th className="w-[24%] px-4 py-3 font-semibold">Name</th>
              <th className="w-[18%] px-4 py-3 font-semibold">Servers</th>
              <th className="w-[10%] px-4 py-3 font-semibold">Status</th>
              <th className="w-[16%] px-4 py-3 font-semibold">Created / expires</th>
              <th className="w-[32%] px-4 py-3 text-right font-semibold">Actions</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-stone-200">
            {visibleTokens.map((token) => {
              const status = tokenStatus(token);
              const revoked = Boolean(token.revoked_at);
              const inactive = status !== "active";
              const permissions = permissionState.data[token.id] || {};
              const allowedCount = servers.data.filter((server) => Boolean(effectiveRule(permissions[server.id]))).length;
              return (
                <tr className="hover:bg-stone-50" key={token.id}>
                  <td className="px-4 py-4">
                    <div className="grid min-w-0 gap-1">
                      <div className="flex min-w-0 items-center gap-2">
                        <TicketCheck className="h-4 w-4 shrink-0 text-stone-500" />
                        <span className="truncate font-semibold">{token.name}</span>
                      </div>
                      <div className="flex min-w-0 items-center gap-2 pl-6">
                        <span className="truncate font-mono text-xs text-stone-500">{maskedToken(token.token)}</span>
                        <CopyButton
                          value={token.token}
                          variant="ghost"
                          className="h-7 w-7 shrink-0 px-0"
                          title={token.token ? "Copy token" : "Token is show-once or reusable copy is disabled"}
                          iconClassName="h-3.5 w-3.5"
                          disabled={!token.token}
                        >
                          {null}
                        </CopyButton>
                      </div>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <div className="grid gap-1.5">
                      <div className="flex flex-wrap gap-1.5">
                        {servers.data.map((server) => {
                          const permission = permissions[server.id];
                          const rule = effectiveRule(permission);
                          return (
                            <button
                              type="button"
                              key={server.id}
                              title={`${server.name}: ${ruleLabel(rule)}${rule ? ` - ${permissionLifetimeLabel(permission)}` : ""}`}
                              className={`h-4 w-4 rounded-full border border-white shadow-sm ring-1 ring-stone-200 ${ruleDotClass(rule)}`}
                              onClick={() => setPermissionDialog({ token, server })}
                              disabled={inactive}
                            />
                          );
                        })}
                      </div>
                      <span className="text-[11px] text-stone-500">
                        {servers.data.length === 0 ? "No servers" : `${allowedCount}/${servers.data.length} servers`}
                      </span>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <Badge tone={status === "active" ? "good" : status === "expired" ? "warn" : "bad"}>{status}</Badge>
                  </td>
                  <td className="px-4 py-4 text-xs text-stone-500">
                    <div className="grid gap-1">
                      <span className="inline-flex items-center gap-1.5">
                        <CalendarClock className="h-3.5 w-3.5" />
                        {formatDate(token.created_at)}
                      </span>
                      <span>{token.expires_at ? `Expires ${formatDate(token.expires_at)}` : "Never expires"}</span>
                    </div>
                  </td>
                  <td className="px-4 py-4">
                    <div className="flex justify-end gap-2">
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-3"
                        onClick={() => setConnectorPermissionDialog(token)}
                        disabled={inactive}
                        title="Set this token's connector action permissions"
                      >
                        <Database className="h-4 w-4" />
                        Connectors
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-3"
                        onClick={() => setBulkDialog({ open: true, token, rule: "approval_required", lifetime: "permanent" })}
                        disabled={inactive || servers.data.length === 0 || permissionState.savingKey === `${token.id}:all`}
                        title="Set this token's permission for all servers"
                      >
                        Bulk
                      </Button>
                      <Button
                        type="button"
                        variant="outline"
                        className="h-9 px-3"
                        onClick={() => setInstallDialog({ open: true, token, provider: "manual" })}
                        disabled={inactive || !token.token}
                        title={token.token ? "Install MCP with this token" : "Create a new token or enable reusable token copy in Settings"}
                      >
                        <PlugZap className="h-4 w-4" />
                        Install
                      </Button>
                      <Button
                        type="button"
                        variant="danger"
                        className="h-9 px-3"
                        onClick={() => revokeToken(token)}
                        disabled={revoked || state.state === "revoking"}
                      >
                        Revoke
                      </Button>
                    </div>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        {tokens.state === "ready" && tokens.data.length === 0 ? (
          <div className="p-4">
            <Notice>Create your first API token.</Notice>
          </div>
        ) : null}
        {tokens.state === "ready" && tokens.data.length > 0 && visibleTokens.length === 0 ? (
          <div className="p-4">
            <Notice>No {tokenFilter} tokens.</Notice>
          </div>
        ) : null}
      </div>

      <Drawer
        open={drawerOpen}
        title="Add API token"
        description="Use one token per AI client, laptop, or temporary maintenance session."
        onClose={() => setDrawerOpen(false)}
      >
        <form className="grid gap-4" onSubmit={createToken}>
          <Field>
            Name
            <Input value={form.name} onChange={(event) => setForm({ ...form, name: event.target.value })} required />
          </Field>
          <Field>
            Expiration
            <Select value={form.expires_in} onChange={(event) => setForm({ ...form, expires_in: event.target.value })}>
              {tokenExpiryOptions.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </Select>
          </Field>
          {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
          <Button type="submit" disabled={state.state === "saving"}>
            <Plus className="h-4 w-4" />
            {state.state === "saving" ? "Creating..." : "Create token"}
          </Button>
          <Notice>Use short-lived tokens for temporary maintenance. By default the token is shown once after creation.</Notice>
        </form>
      </Drawer>

      <PermissionDialog value={permissionDialog} permissionState={permissionState} onClose={() => setPermissionDialog(null)} onSetRule={setTokenServerRule} />
      <ConnectorPermissionDialog token={connectorPermissionDialog} onClose={() => setConnectorPermissionDialog(null)} />
      <Dialog
        open={bulkDialog.open}
        title="Set all server permissions"
        description={bulkDialog.token ? `Apply one permission rule to every server for ${bulkDialog.token.name}.` : ""}
        onClose={() => setBulkDialog({ open: false, token: null, rule: "approval_required", lifetime: "permanent" })}
        size="md"
      >
        <form className="grid gap-4" onSubmit={applyBulkPermissions}>
          <Notice>Use this for broad maintenance windows. You can still override individual servers afterwards.</Notice>
          <Field>
            Permission rule
            <Select value={bulkDialog.rule} onChange={(event) => setBulkDialog((current) => ({ ...current, rule: event.target.value }))}>
              <option value="">Disabled for all servers</option>
              <option value="approval_required">Prompt for all servers</option>
              <option value="always_run">Always run for all servers</option>
            </Select>
          </Field>
          <Field>
            Lifetime
            <Select
              value={bulkDialog.lifetime}
              onChange={(event) => setBulkDialog((current) => ({ ...current, lifetime: event.target.value }))}
              disabled={!bulkDialog.rule}
            >
              <option value="permanent">Permanent</option>
              <option value="1h">1 hour</option>
              <option value="4h">4 hours</option>
              <option value="1d">1 day</option>
            </Select>
          </Field>
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={() => setBulkDialog({ open: false, token: null, rule: "approval_required", lifetime: "permanent" })}>
              Cancel
            </Button>
            <Button type="submit" disabled={!bulkDialog.token || permissionState.savingKey === `${bulkDialog.token?.id}:all`}>
              Apply to all servers
            </Button>
          </div>
        </form>
      </Dialog>
      <TokenInstallDialog
        state={installDialog}
        onChange={setInstallDialog}
        onClose={() => setInstallDialog({ open: false, token: null, provider: "manual" })}
      />
    </section>
  );
}

function TokenStat({ icon: Icon, label, value, tone = "neutral", selected = false, onClick }) {
  const tones = {
    neutral: selected ? "token-stat-neutral-selected border-stone-500 bg-stone-50 text-stone-950 ring-stone-300" : "token-stat-neutral border-stone-200 bg-white text-stone-900 ring-transparent",
    good: selected
      ? "token-stat-good-selected border-emerald-700 bg-emerald-50 text-emerald-950 ring-emerald-200"
      : "token-stat-good border-emerald-200 bg-emerald-50 text-emerald-950 ring-transparent",
    warn: selected ? "token-stat-warn-selected border-amber-700 bg-amber-50 text-amber-950 ring-amber-200" : "token-stat-warn border-amber-200 bg-amber-50 text-amber-950 ring-transparent",
    bad: selected ? "token-stat-bad-selected border-red-700 bg-red-50 text-red-950 ring-red-200" : "token-stat-bad border-red-200 bg-red-50 text-red-950 ring-transparent",
  };
  return (
    <button
      type="button"
      className={`flex items-center justify-between rounded-lg border px-4 py-3 text-left ring-2 ring-offset-1 transition hover:-translate-y-0.5 hover:shadow-sm ${tones[tone]}`}
      onClick={onClick}
    >
      <div>
        <p className="text-xs font-semibold uppercase text-stone-500">{label}</p>
        <p className="mt-1 text-2xl font-semibold">{value}</p>
      </div>
      <Icon className="h-5 w-5 text-stone-500" />
    </button>
  );
}

function tokenCreatePayload(form) {
  const payload = { name: form.name };
  const option = tokenExpiryOptions.find((item) => item.value === form.expires_in);
  if (option?.ms) {
    payload.expires_at = new Date(Date.now() + option.ms).toISOString();
  }
  return payload;
}

function tokenStatus(token) {
  if (token.revoked_at) return "revoked";
  if (token.expires_at && new Date(token.expires_at).getTime() <= Date.now()) return "expired";
  return "active";
}

function formatDate(value) {
  if (!value) return "-";
  return new Intl.DateTimeFormat(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}
