import { Clock3, Trash2 } from "lucide-react";
import { useEffect, useEffectEvent, useState } from "react";
import { Badge } from "../../../components/ui/badge";
import { Button } from "../../../components/ui/button";
import { CopyButton } from "../../../components/ui/copy-button";
import { Dialog } from "../../../components/ui/dialog";
import { Field, Input } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { TerminalBlock } from "../../../components/ui/terminal-block";

const initialForm = {
  ruleId: "aipermission-expiration",
  prefix: "",
  expireCurrentDays: 0,
  expireNoncurrentDays: 0,
  abortMultipartDays: 7,
  enabled: true,
  acknowledged: false,
};

export function S3LifecycleDialog({ open, bucket, theme, inputClass, borderClass, mutedClass, onClose, onRun }) {
  const [policy, setPolicy] = useState(null);
  const [form, setForm] = useState(initialForm);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState(false);
  const loadForEffect = useEffectEvent(loadPolicy);

  useEffect(() => {
    if (!open) return;
    setPolicy(null);
    setForm(initialForm);
    setError("");
    setConfirmDelete(false);
    void loadForEffect();
  }, [open, bucket]);

  if (!open) return null;

  async function loadPolicy() {
    setPending(true);
    setError("");
    try {
      const item = await onRun({
        actionName: "get_bucket_lifecycle",
        input: {},
        reason: "manual S3 lifecycle review",
        busy: "reading lifecycle",
      });
      if (!item) return;
      setPolicy(item.output || { configured: false, rules: [], raw_xml: "" });
    } catch (loadError) {
      setError(loadError.message || "Bucket lifecycle could not be read.");
    } finally {
      setPending(false);
    }
  }

  async function replacePolicy(event) {
    event.preventDefault();
    if (pending || !form.acknowledged) return;
    const values = [form.expireCurrentDays, form.expireNoncurrentDays, form.abortMultipartDays].map(Number);
    if (values.some((value) => !Number.isInteger(value) || value < 0 || value > 36500) || values.every((value) => value === 0)) {
      setError("Use whole-day values from 0 to 36500 and configure at least one lifecycle behavior.");
      return;
    }
    setPending(true);
    setError("");
    try {
      const item = await onRun({
        actionName: "replace_bucket_lifecycle",
        input: {
          rule_id: form.ruleId,
          prefix: form.prefix,
          expire_current_after_days: values[0],
          expire_noncurrent_after_days: values[1],
          abort_incomplete_multipart_days: values[2],
          enabled: form.enabled,
        },
        reason: "manual S3 lifecycle policy replacement",
        busy: "replacing lifecycle",
      });
      if (!item) {
        setPending(false);
        return;
      }
      setForm((current) => ({ ...current, acknowledged: false }));
      await loadPolicy();
    } catch (replaceError) {
      setError(replaceError.message || "Bucket lifecycle replacement failed.");
      setPending(false);
    }
  }

  async function deletePolicy() {
    if (pending || !confirmDelete) return;
    setPending(true);
    setError("");
    try {
      const item = await onRun({
        actionName: "delete_bucket_lifecycle",
        input: {},
        reason: "manual S3 lifecycle policy deletion",
        busy: "deleting lifecycle",
      });
      if (!item) {
        setPending(false);
        return;
      }
      setConfirmDelete(false);
      await loadPolicy();
    } catch (deleteError) {
      setError(deleteError.message || "Bucket lifecycle deletion failed.");
      setPending(false);
    }
  }

  const panelClass = theme === "light" ? "bg-stone-50" : "bg-stone-900";
  const rules = Array.isArray(policy?.rules) ? policy.rules : [];
  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="S3 bucket lifecycle"
      description={bucket || "S3 bucket"}
      size="wide"
      closeDisabled={pending}
      closeOnOverlay={false}
    >
      <div className="grid max-h-[calc(100vh-160px)] min-h-0 gap-4 overflow-auto lg:grid-cols-2">
        <section className="grid content-start gap-3">
          <div className="flex items-center justify-between gap-3">
            <div>
              <p className="text-sm font-semibold">Current policy</p>
              <p className={`text-xs ${mutedClass}`}>
                {policy?.configured ? `${rules.length} lifecycle rule(s)` : "No lifecycle policy configured"}
              </p>
            </div>
            <Button type="button" variant="outline" className="h-8" onClick={loadPolicy} disabled={pending}>
              Refresh
            </Button>
          </div>
          <div className={`grid min-h-48 gap-2 rounded-lg border p-3 ${borderClass} ${panelClass}`}>
            {rules.length === 0 ? (
              <div className="grid min-h-40 place-items-center text-center">
                <p className={`text-sm ${mutedClass}`}>{pending ? "Reading policy..." : "No rules to display."}</p>
              </div>
            ) : (
              rules.map((rule) => (
                <div className={`grid gap-2 rounded-md border p-3 ${borderClass}`} key={rule.id}>
                  <div className="flex items-center justify-between gap-3">
                    <p className="font-mono text-xs font-semibold">{rule.id || "unnamed rule"}</p>
                    <Badge tone={rule.status === "Enabled" ? "good" : "neutral"}>{String(rule.status || "unknown").toLowerCase()}</Badge>
                  </div>
                  <p className={`text-xs ${mutedClass}`}>Prefix: {rule.prefix || "whole bucket"}</p>
                  <div className="grid grid-cols-3 gap-2 text-xs">
                    <span>Current: {dayLabel(rule.expire_current_after_days)}</span>
                    <span>Noncurrent: {dayLabel(rule.expire_noncurrent_after_days)}</span>
                    <span>Multipart: {dayLabel(rule.abort_incomplete_multipart_days)}</span>
                  </div>
                </div>
              ))
            )}
          </div>
          {policy?.raw_xml ? (
            <div className="grid gap-2">
              <div className="flex items-center justify-between gap-3">
                <p className="text-xs font-semibold uppercase text-stone-400">Raw lifecycle XML</p>
                <CopyButton value={policy.raw_xml} variant="outline" className="h-8 px-2 text-xs" />
              </div>
              <TerminalBlock className="max-h-44 overflow-auto whitespace-pre-wrap text-xs" surface="log">
                {policy.raw_xml}
              </TerminalBlock>
            </div>
          ) : null}
          {policy?.configured ? (
            <div className="grid gap-2">
              <label className={`flex items-start gap-2 rounded-md border p-3 text-sm ${borderClass}`}>
                <input
                  type="checkbox"
                  checked={confirmDelete}
                  onChange={(event) => setConfirmDelete(event.target.checked)}
                  disabled={pending}
                />
                remove the complete lifecycle policy from this bucket
              </label>
              <Button type="button" variant="danger" onClick={deletePolicy} disabled={pending || !confirmDelete}>
                <Trash2 className="h-4 w-4" /> Delete lifecycle policy
              </Button>
            </div>
          ) : null}
        </section>
        <form className="grid content-start gap-3" onSubmit={replacePolicy}>
          <div>
            <p className="text-sm font-semibold">Replace policy</p>
            <p className={`text-xs ${mutedClass}`}>Create exactly one bounded expiration and cleanup rule.</p>
          </div>
          <Notice tone="bad">This replaces every existing lifecycle rule, including rules created outside AIPermission.</Notice>
          <Field>
            Rule ID
            <Input
              className={inputClass}
              value={form.ruleId}
              maxLength={255}
              onChange={(event) => setForm({ ...form, ruleId: event.target.value })}
            />
          </Field>
          <Field>
            Object prefix
            <Input
              className={inputClass}
              value={form.prefix}
              maxLength={1024}
              onChange={(event) => setForm({ ...form, prefix: event.target.value })}
              placeholder="Empty applies to the whole bucket"
            />
          </Field>
          <div className="grid gap-3 sm:grid-cols-3">
            <Field>
              Current days
              <Input
                className={inputClass}
                type="number"
                min="0"
                max="36500"
                value={form.expireCurrentDays}
                onChange={(event) => setForm({ ...form, expireCurrentDays: event.target.value })}
              />
            </Field>
            <Field>
              Noncurrent days
              <Input
                className={inputClass}
                type="number"
                min="0"
                max="36500"
                value={form.expireNoncurrentDays}
                onChange={(event) => setForm({ ...form, expireNoncurrentDays: event.target.value })}
              />
            </Field>
            <Field>
              Multipart days
              <Input
                className={inputClass}
                type="number"
                min="0"
                max="36500"
                value={form.abortMultipartDays}
                onChange={(event) => setForm({ ...form, abortMultipartDays: event.target.value })}
              />
            </Field>
          </div>
          <label className={`flex items-center gap-2 rounded-md border px-3 py-2 text-sm ${borderClass}`}>
            <input
              type="checkbox"
              checked={form.enabled}
              onChange={(event) => setForm({ ...form, enabled: event.target.checked })}
              disabled={pending}
            />{" "}
            enable replacement rule
          </label>
          <label className={`flex items-start gap-2 rounded-md border p-3 text-sm ${borderClass}`}>
            <input
              type="checkbox"
              checked={form.acknowledged}
              onChange={(event) => setForm({ ...form, acknowledged: event.target.checked })}
              disabled={pending}
            />
            I understand that this replaces the complete lifecycle policy.
          </label>
          {error ? <Notice tone="bad">{error}</Notice> : null}
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={onClose} disabled={pending}>
              Close
            </Button>
            <Button type="submit" variant="danger" disabled={pending || !form.acknowledged || !form.ruleId.trim()}>
              {pending ? "Working..." : "Replace lifecycle policy"}
            </Button>
          </div>
        </form>
      </div>
    </Dialog>
  );
}

export function LifecycleIcon() {
  return <Clock3 className="h-3.5 w-3.5" />;
}

function dayLabel(value) {
  const days = Number(value || 0);
  return days > 0 ? `${days}d` : "off";
}
