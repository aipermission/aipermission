import { Checkbox, Field, Select, Textarea } from "../../../components/ui/form";
import { CopyButton } from "../../../components/ui/copy-button";
import { Copy } from "lucide-react";
import { cleanupMethods } from "./cleanup-types";
import type { CleanupEvidence, CleanupMethod, CleanupSubject } from "./cleanup-types";

const methodLabels: Record<CleanupMethod, string> = {
  provider_console: "Provider console",
  independent_admin_session: "Independent admin session",
  decommissioned_location: "Decommissioned location",
};

export function CleanupSubjectFields({
  subject,
  evidence,
  disabled,
  onChange,
}: {
  subject: CleanupSubject;
  evidence: CleanupEvidence;
  disabled: boolean;
  onChange: (_value: CleanupEvidence) => void;
}) {
  return (
    <fieldset disabled={disabled} className="grid min-w-0 gap-3 border-t border-stone-200 py-4">
      <legend className="break-all pt-4 text-sm font-semibold text-stone-900">
        {subject.username}@{subject.host}:{subject.port}
      </legend>
      <div className="flex min-w-0 items-center gap-2">
        <code className="min-w-0 flex-1 break-all text-xs">{subject.key_fingerprint}</code>
        <CopyButton value={subject.key_fingerprint} variant="outline" className="h-8 w-8 px-0" title="Copy key fingerprint">
          <Copy className="h-4 w-4" />
        </CopyButton>
      </div>
      <div className="grid gap-1 text-xs text-stone-500">
        <span>Recorded host fingerprints</span>
        {subject.host_fingerprints.map((pin) => (
          <code key={pin} className="break-all">
            {pin}
          </code>
        ))}
      </div>
      <Field>
        External verification method
        <Select value={evidence.method} onChange={(event) => onChange({ ...evidence, method: event.target.value as CleanupMethod })}>
          {cleanupMethods.map((method) => (
            <option key={method} value={method}>
              {methodLabels[method]}
            </option>
          ))}
        </Select>
      </Field>
      <Field>
        Location evidence
        <Textarea maxLength={2000} value={evidence.reason} onChange={(event) => onChange({ ...evidence, reason: event.target.value })} />
      </Field>
      <label className="flex gap-2 text-sm text-stone-800">
        <Checkbox checked={evidence.absent} onChange={(event) => onChange({ ...evidence, absent: event.target.checked })} />I independently
        verified this exact key is absent from this location.
      </label>
    </fieldset>
  );
}
