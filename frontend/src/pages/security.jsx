import { Plus, Trash2 } from "lucide-react";
import { useEffect, useState } from "react";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { Button } from "../components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../components/ui/card";
import { Field, Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";

const emptyState = { state: "idle", error: null, message: null };
const defaultSecurity = { reusable_tokens: false, expose_mcp_server_metadata: false, mcp_start_enabled: false, redaction_mode: "basic" };

export function SecurityPage() {
  const [security, setSecurity] = useState({ state: "loading", data: defaultSecurity, error: null });
  const { actionState: securityState, runAction: runSecurityAction } = useAsyncAction(emptyState);
  const [redactionRules, setRedactionRules] = useState({ state: "loading", data: [], error: null });
  const { actionState: redactionRuleState, runAction: runRedactionRuleAction } = useAsyncAction(emptyState);
  const [redactionRuleForm, setRedactionRuleForm] = useState({ name: "", pattern: "", enabled: true });

  async function loadSecurity() {
    try {
      const data = await apiGet("/api/settings/security");
      setSecurity({ state: "ready", data, error: null });
    } catch (error) {
      setSecurity({ state: "error", data: defaultSecurity, error: error.message });
    }
  }

  async function loadRedactionRules() {
    try {
      const data = await apiGet("/api/settings/redaction-rules");
      setRedactionRules({ state: "ready", data, error: null });
    } catch (error) {
      setRedactionRules({ state: "error", data: [], error: error.message });
    }
  }

  useEffect(() => {
    void loadSecurity();
    void loadRedactionRules();
  }, []);

  async function updateSecuritySettings(patch, message) {
    const next = { ...security.data, ...patch };
    await runSecurityAction({
      pending: "saving",
      successMessage: message,
      action: async () => {
        const data = await apiPut("/api/settings/security", next);
        setSecurity({ state: "ready", data, error: null });
      },
    });
  }

  function updateRedactionRuleForm(field, value) {
    setRedactionRuleForm((current) => ({ ...current, [field]: value }));
  }

  async function createRedactionRule(event) {
    event.preventDefault();
    await runRedactionRuleAction({
      pending: "saving",
      successMessage: "Custom redaction rule added.",
      action: async () => {
        await apiPost("/api/settings/redaction-rules", redactionRuleForm);
        setRedactionRuleForm({ name: "", pattern: "", enabled: true });
        await loadRedactionRules();
      },
    });
  }

  async function toggleRedactionRule(rule, enabled) {
    await runRedactionRuleAction({
      pending: "saving",
      successMessage: enabled ? "Custom rule enabled." : "Custom rule disabled.",
      action: async () => {
        await apiPut(`/api/settings/redaction-rules/${rule.id}`, { name: rule.name, pattern: rule.pattern, enabled });
        await loadRedactionRules();
      },
    });
  }

  async function deleteRedactionRule(rule) {
    await runRedactionRuleAction({
      pending: "deleting",
      successMessage: "Custom redaction rule deleted.",
      action: async () => {
        await apiDelete(`/api/settings/redaction-rules/${rule.id}`);
        await loadRedactionRules();
      },
    });
  }

  return (
    <section className="mx-auto grid w-full max-w-2xl gap-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-lg font-semibold">Security</h3>
          <p className="text-sm text-stone-500">Control token copy behavior, MCP metadata exposure, and redaction rules.</p>
        </div>
      </div>

      {security.state === "error" ? <Notice tone="bad">{security.error}</Notice> : null}
      {securityState.message ? <Notice tone="good">{securityState.message}</Notice> : null}
      {securityState.state === "error" ? <Notice tone="bad">{securityState.error}</Notice> : null}
      {redactionRuleState.message ? <Notice tone="good">{redactionRuleState.message}</Notice> : null}
      {redactionRuleState.state === "error" ? <Notice tone="bad">{redactionRuleState.error}</Notice> : null}

      <Card>
        <CardHeader>
          <CardTitle>API tokens</CardTitle>
          <CardDescription>Choose whether token values can be copied again after creation.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-4">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 rounded border-stone-300 accent-emerald-900"
              checked={Boolean(security.data?.reusable_tokens)}
              disabled={security.state === "loading" || securityState.state === "saving"}
              onChange={(event) =>
                updateSecuritySettings(
                  { reusable_tokens: event.target.checked },
                  event.target.checked
                    ? "Reusable token copy is enabled for newly created tokens."
                    : "Reusable token copy is disabled. Stored reusable token values were cleared.",
                )
              }
            />
            <span className="grid gap-1 text-sm">
              <span className="font-semibold text-stone-900">Allow reusable token copy</span>
              <span className="text-stone-500">
                Off is safer: new token values are shown once at creation. Turn this on only if you want local convenience for newly created MCP tokens.
              </span>
            </span>
          </label>
          {!security.data?.reusable_tokens ? (
            <Notice>Reusable token copy is off. Copy new tokens from the creation message before closing it.</Notice>
          ) : (
            <Notice tone="warn">Reusable token values are stored in the encrypted local database for easier local MCP setup.</Notice>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>MCP exposure</CardTitle>
          <CardDescription>Choose how much connector endpoint inventory metadata MCP clients can see.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-4">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 rounded border-stone-300 accent-emerald-900"
              checked={Boolean(security.data?.expose_mcp_server_metadata)}
              disabled={security.state === "loading" || securityState.state === "saving"}
              onChange={(event) =>
                updateSecuritySettings(
                  { expose_mcp_server_metadata: event.target.checked },
                  event.target.checked
                    ? "MCP connector targets now include endpoint metadata."
                    : "MCP connector targets now return minimal endpoint metadata.",
                )
              }
            />
            <span className="grid gap-1 text-sm">
              <span className="font-semibold text-stone-900">Expose endpoint metadata to MCP</span>
              <span className="text-stone-500">
                Off is safer: AI clients see connector target/profile/action permission context only. Turn this on only when the agent needs connector endpoint context.
              </span>
            </span>
          </label>
          {!security.data?.expose_mcp_server_metadata ? (
            <Notice>MCP connector targets hide endpoint inventory details by default.</Notice>
          ) : (
            <Notice tone="warn">Endpoint metadata is visible to any MCP client using an allowed token.</Notice>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>MCP startup</CardTitle>
          <CardDescription>Choose whether MCP command execution starts automatically after this database is unlocked.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-4">
            <input
              type="checkbox"
              className="mt-1 h-4 w-4 rounded border-stone-300 accent-emerald-900"
              checked={Boolean(security.data?.mcp_start_enabled)}
              disabled={security.state === "loading" || securityState.state === "saving"}
              onChange={(event) =>
                updateSecuritySettings(
                  { mcp_start_enabled: event.target.checked },
                  event.target.checked
                    ? "MCP execution will start automatically after unlock."
                    : "MCP execution will start stopped after unlock.",
                )
              }
            />
            <span className="grid gap-1 text-sm">
              <span className="font-semibold text-stone-900">Start MCP execution after unlock</span>
              <span className="text-stone-500">
                Off is safer: permissions stay saved, but MCP command execution starts stopped when the gateway starts or the database is unlocked.
              </span>
            </span>
          </label>
          {!security.data?.mcp_start_enabled ? (
            <Notice>MCP execution starts stopped by default. Use the sidebar Start MCP button when you are ready.</Notice>
          ) : (
            <Notice tone="warn">MCP execution will start automatically for this database after unlock.</Notice>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Redaction</CardTitle>
          <CardDescription>Mask common secrets before command and audit data is stored or returned through MCP.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Field>
            Redaction mode
            <select
              className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm outline-none focus:border-emerald-800"
              value={security.data?.redaction_mode || "basic"}
              disabled={security.state === "loading" || securityState.state === "saving"}
              onChange={(event) => updateSecuritySettings({ redaction_mode: event.target.value }, `Redaction mode set to ${event.target.value}.`)}
            >
              <option value="basic">Basic</option>
              <option value="off">Off</option>
            </select>
          </Field>
          <Notice tone={security.data?.redaction_mode === "off" ? "warn" : "good"}>
            Basic redaction masks common token, password, API key, bearer token, and private key patterns in persisted command/audit data. Avoid printing secrets; redaction is best-effort.
          </Notice>
          {security.data?.redaction_mode === "basic" ? (
            <div className="grid gap-4 rounded-lg border border-stone-200 bg-stone-50 p-4">
              <div>
                <h4 className="text-sm font-semibold text-stone-900">Custom redaction rules</h4>
                <p className="mt-1 text-sm text-stone-500">Add Go RE2 regex patterns that run after the built-in basic rules. Matches are replaced with [REDACTED].</p>
              </div>
              <form className="grid gap-3" onSubmit={createRedactionRule}>
                <Field>
                  Rule name
                  <Input value={redactionRuleForm.name} onChange={(event) => updateRedactionRuleForm("name", event.target.value)} placeholder="Internal token" maxLength={80} required />
                </Field>
                <Field>
                  Regex pattern
                  <Input value={redactionRuleForm.pattern} onChange={(event) => updateRedactionRuleForm("pattern", event.target.value)} placeholder="(?i)internal_[a-z0-9]{24,}" required />
                </Field>
                <label className="flex items-center gap-2 text-sm text-stone-700">
                  <input
                    type="checkbox"
                    className="h-4 w-4 rounded border-stone-300 accent-emerald-900"
                    checked={redactionRuleForm.enabled}
                    onChange={(event) => updateRedactionRuleForm("enabled", event.target.checked)}
                  />
                  Enabled
                </label>
                <Button type="submit" variant="outline" disabled={redactionRuleState.state === "saving"}>
                  <Plus className="h-4 w-4" />
                  Add rule
                </Button>
              </form>
              {redactionRules.state === "error" ? <Notice tone="bad">{redactionRules.error}</Notice> : null}
              <div className="grid gap-2">
                {redactionRules.data.map((rule) => (
                  <div key={rule.id} className="grid gap-2 rounded-md border border-stone-200 bg-white p-3">
                    <div className="flex items-start justify-between gap-3">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-semibold text-stone-900">{rule.name}</p>
                        <code className="mt-1 block break-all rounded bg-stone-100 px-2 py-1 text-xs text-stone-700">{rule.pattern}</code>
                      </div>
                      <Button type="button" variant="ghost" className="h-8 w-8 shrink-0 px-0" onClick={() => deleteRedactionRule(rule)} disabled={redactionRuleState.state === "deleting"}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                    <label className="flex items-center gap-2 text-xs font-medium text-stone-600">
                      <input
                        type="checkbox"
                        className="h-4 w-4 rounded border-stone-300 accent-emerald-900"
                        checked={rule.enabled}
                        onChange={(event) => toggleRedactionRule(rule, event.target.checked)}
                        disabled={redactionRuleState.state === "saving"}
                      />
                      {rule.enabled ? "Enabled" : "Disabled"}
                    </label>
                  </div>
                ))}
                {redactionRules.state === "ready" && redactionRules.data.length === 0 ? <Notice>No custom redaction rules yet.</Notice> : null}
              </div>
            </div>
          ) : (
            <Notice>Custom redaction rules are available when redaction mode is Basic.</Notice>
          )}
        </CardContent>
      </Card>
    </section>
  );
}
