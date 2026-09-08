import { Plus, Trash2 } from "lucide-react";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const toggleSettings = {
  reusable_tokens: {
    title: "API tokens",
    description: "Choose whether token values can be copied again after creation.",
    label: "Allow reusable token copy",
    detail:
      "Off is safer: new token values are shown once at creation. Turn this on only if you want local convenience for newly created MCP tokens.",
    offNotice: "Reusable token copy is off. Copy new tokens from the creation message before closing it.",
    onNotice: "Reusable token values are stored in the encrypted local database for easier local MCP setup.",
    enabledMessage: "Reusable token copy is enabled for newly created tokens.",
    disabledMessage: "Reusable token copy is disabled. Stored reusable token values were cleared.",
  },
  expose_mcp_server_metadata: {
    title: "MCP exposure",
    description: "Choose how much connector endpoint inventory metadata MCP clients can see.",
    label: "Expose endpoint metadata to MCP",
    detail:
      "Off is safer: AI clients see connector target/profile/action permission context only. Turn this on only when the agent needs connector endpoint context.",
    offNotice: "MCP connector targets hide endpoint inventory details by default.",
    onNotice: "Endpoint metadata is visible to any MCP client using an allowed token.",
    enabledMessage: "MCP connector targets now include endpoint metadata.",
    disabledMessage: "MCP connector targets now return minimal endpoint metadata.",
  },
  mcp_start_enabled: {
    title: "MCP startup",
    description: "Choose whether MCP command execution starts automatically after this database is unlocked.",
    label: "Start MCP execution after unlock",
    detail:
      "Off is safer: permissions stay saved, but MCP command execution starts stopped when the gateway starts or the database is unlocked.",
    offNotice: "MCP execution starts stopped by default. Use the sidebar Start MCP button when you are ready.",
    onNotice: "MCP execution will start automatically for this database after unlock.",
    enabledMessage: "MCP execution will start automatically after unlock.",
    disabledMessage: "MCP execution will start stopped after unlock.",
  },
};

export function SecurityToggleCard({ setting, value, disabled, onUpdate }) {
  const copy = toggleSettings[setting];
  return (
    <Card>
      <CardHeader>
        <CardTitle>{copy.title}</CardTitle>
        <CardDescription>{copy.description}</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <label className="flex items-start gap-3 rounded-md border border-stone-200 bg-stone-50 p-4">
          <input
            type="checkbox"
            aria-label={copy.label}
            className="mt-1 h-4 w-4 rounded border-stone-300 accent-emerald-900"
            checked={Boolean(value)}
            disabled={disabled}
            onChange={(event) =>
              onUpdate({ [setting]: event.target.checked }, event.target.checked ? copy.enabledMessage : copy.disabledMessage)
            }
          />
          <span className="grid gap-1 text-sm">
            <span className="font-semibold text-stone-900">{copy.label}</span>
            <span className="text-stone-500">{copy.detail}</span>
          </span>
        </label>
        <Notice tone={value ? "warn" : undefined}>{value ? copy.onNotice : copy.offNotice}</Notice>
      </CardContent>
    </Card>
  );
}

export function RedactionSettingsCard({ security, action, rules, form, onUpdateSecurity, onUpdateForm, onCreate, onToggle, onDelete }) {
  const busy = security.state === "loading" || action.state === "saving";
  const basic = security.data?.redaction_mode === "basic";
  return (
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
            disabled={busy}
            onChange={(event) => onUpdateSecurity({ redaction_mode: event.target.value }, `Redaction mode set to ${event.target.value}.`)}
          >
            <option value="basic">Basic</option>
            <option value="off">Off</option>
          </select>
        </Field>
        <Notice tone={basic ? "good" : "warn"}>
          Basic redaction masks common token, password, API key, bearer token, and private key patterns in persisted command/audit data.
          Avoid printing secrets; redaction is best-effort.
        </Notice>
        {basic ? (
          <RedactionRuleEditor
            rules={rules}
            action={action}
            form={form}
            onUpdate={onUpdateForm}
            onCreate={onCreate}
            onToggle={onToggle}
            onDelete={onDelete}
          />
        ) : (
          <Notice>Custom redaction rules are available when redaction mode is Basic.</Notice>
        )}
      </CardContent>
    </Card>
  );
}

function RedactionRuleEditor({ rules, action, form, onUpdate, onCreate, onToggle, onDelete }) {
  return (
    <div className="grid gap-4 rounded-lg border border-stone-200 bg-stone-50 p-4">
      <div>
        <h4 className="text-sm font-semibold text-stone-900">Custom redaction rules</h4>
        <p className="mt-1 text-sm text-stone-500">
          Add Go RE2 regex patterns that run after the built-in basic rules. Matches are replaced with [REDACTED].
        </p>
      </div>
      <form className="grid gap-3" onSubmit={onCreate}>
        <Field>
          Rule name
          <Input
            value={form.name}
            onChange={(event) => onUpdate("name", event.target.value)}
            placeholder="Internal token"
            maxLength={80}
            required
          />
        </Field>
        <Field>
          Regex pattern
          <Input
            value={form.pattern}
            onChange={(event) => onUpdate("pattern", event.target.value)}
            placeholder="(?i)internal_[a-z0-9]{24,}"
            required
          />
        </Field>
        <label className="flex items-center gap-2 text-sm text-stone-700">
          <input
            type="checkbox"
            className="h-4 w-4 rounded border-stone-300 accent-emerald-900"
            checked={form.enabled}
            onChange={(event) => onUpdate("enabled", event.target.checked)}
          />
          Enabled
        </label>
        <Button type="submit" variant="outline" disabled={action.state === "saving"}>
          <Plus className="h-4 w-4" />
          Add rule
        </Button>
      </form>
      {rules.state === "error" ? <Notice tone="bad">{rules.error}</Notice> : null}
      <RedactionRuleList rules={rules} action={action} onToggle={onToggle} onDelete={onDelete} />
    </div>
  );
}

function RedactionRuleList({ rules, action, onToggle, onDelete }) {
  if (rules.state === "ready" && rules.data.length === 0) return <Notice>No custom redaction rules yet.</Notice>;
  return (
    <div className="grid gap-2">
      {rules.data.map((rule) => (
        <div key={rule.id} className="grid gap-2 rounded-md border border-stone-200 bg-white p-3">
          <div className="flex items-start justify-between gap-3">
            <div className="min-w-0">
              <p className="truncate text-sm font-semibold text-stone-900">{rule.name}</p>
              <code className="mt-1 block break-all rounded bg-stone-100 px-2 py-1 text-xs text-stone-700">{rule.pattern}</code>
            </div>
            <Button
              type="button"
              variant="ghost"
              className="h-8 w-8 shrink-0 px-0"
              aria-label={`Delete ${rule.name}`}
              title={`Delete ${rule.name}`}
              onClick={() => onDelete(rule)}
              disabled={action.state === "deleting"}
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </div>
          <label className="flex items-center gap-2 text-xs font-medium text-stone-600">
            <input
              type="checkbox"
              className="h-4 w-4 rounded border-stone-300 accent-emerald-900"
              checked={rule.enabled}
              onChange={(event) => onToggle(rule, event.target.checked)}
              disabled={action.state === "saving"}
            />
            {rule.enabled ? "Enabled" : "Disabled"}
          </label>
        </div>
      ))}
    </div>
  );
}
