import { useState } from "react";
import { Button } from "../../../components/ui/button";
import { Field, Select, Textarea } from "../../../components/ui/form";
import { Notice } from "../../../components/ui/notice";
import { errorMessage } from "../../../lib/errors";
import { CleanupSubjectFields } from "./cleanup-subject-fields";
import { CleanupHistory } from "./cleanup-history";
import { cleanupSubmission } from "./cleanup-submission";
import type { CleanupEvidence, CleanupSnapshot, CleanupSubmission } from "./cleanup-types";

export function CleanupProofForm({
  snapshot,
  disabled,
  onSubmit,
}: {
  snapshot: CleanupSnapshot;
  disabled: boolean;
  onSubmit: (_input: CleanupSubmission) => Promise<void>;
}) {
  const [recordID, setRecordID] = useState(snapshot.records[0]?.entry.resource_id);
  const record = snapshot.records.find((value) => value.entry.resource_id === recordID);
  const [choiceDigest, setChoiceDigest] = useState("");
  const choice = record?.choices.find((value) => value.digest === choiceDigest) || record?.choices[0];
  const [evidence, setEvidence] = useState<Record<string, CleanupEvidence>>({});
  const [reason, setReason] = useState("");
  const [error, setError] = useState("");
  const proofFor = (id: string): CleanupEvidence =>
    evidence[id] || { subject_id: id, method: "provider_console", absent: false, reason: "" };
  function reset() {
    setEvidence({});
    setReason("");
    setError("");
  }

  return (
    <form
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        if (disabled || !record || !choice) return;
        try {
          const input = cleanupSubmission(
            snapshot,
            record,
            choice,
            choice.subjects.map((subject) => proofFor(subject.id)),
            reason,
          );
          setError("");
          void onSubmit(input);
        } catch (error) {
          setError(errorMessage(error));
        }
      }}
    >
      <Field>
        Cleanup record
        <Select
          disabled={disabled}
          value={recordID}
          onChange={(event) => {
            setRecordID(Number(event.target.value));
            setChoiceDigest("");
            reset();
          }}
        >
          {snapshot.records.map((value) => (
            <option key={value.entry.resource_id} value={value.entry.resource_id}>
              Record {value.entry.resource_id} / {value.entry.record.status}
            </option>
          ))}
        </Select>
      </Field>
      {record && choice ? (
        <>
          <div className="break-all font-mono text-xs text-stone-500">Generation: {record.entry.record.generation}</div>
          <CleanupHistory entry={record.entry} />
          <Field>
            Selected identity
            <Select
              disabled={disabled}
              value={choice.digest}
              onChange={(event) => {
                setChoiceDigest(event.target.value);
                reset();
              }}
            >
              {record.choices.map((value) => (
                <option key={value.digest} value={value.digest}>
                  {value.identity.username}@{value.identity.host}:{value.identity.port} / {value.digest.slice(0, 12)}
                </option>
              ))}
            </Select>
          </Field>
          {choice.subjects.map((subject) => (
            <CleanupSubjectFields
              key={subject.id}
              subject={subject}
              evidence={proofFor(subject.id)}
              disabled={disabled}
              onChange={(value) => setEvidence((current) => ({ ...current, [subject.id]: value }))}
            />
          ))}
          <Field>
            Decision reason
            <Textarea disabled={disabled} maxLength={2000} value={reason} onChange={(event) => setReason(event.target.value)} />
          </Field>
          {error ? <Notice tone="bad">{error}</Notice> : null}
          <div className="flex justify-end">
            <Button type="submit" disabled={disabled}>
              {disabled ? "Recording..." : "Record external absence"}
            </Button>
          </div>
        </>
      ) : null}
    </form>
  );
}
