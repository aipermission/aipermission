import { useEffect, useRef, useState } from "react";
import { ShieldCheck, Trash2 } from "lucide-react";
import { Button } from "../../../../components/ui/button";
import { Input } from "../../../../components/ui/form";
import { Notice } from "../../../../components/ui/notice";
import { canCleanupRole } from "./role-reconciliation-contract";
import type { DatabaseProfile } from "../../_shared/database-model-types";
import type { RoleHistoryEntry } from "./role-history-types";

export function RoleReconciliationForm({
  entry,
  profiles,
  disabled,
  onConfirm,
}: {
  entry: RoleHistoryEntry;
  profiles: DatabaseProfile[];
  disabled: boolean;
  onConfirm: (_entry: RoleHistoryEntry, _profileID: number, _confirmedRoleName?: string) => Promise<void>;
}) {
  const [open, setOpen] = useState(false);
  const [acknowledgement, setAcknowledgement] = useState<object | null>(null);
  const [confirmation, setConfirmation] = useState<{ owner: object; text: string } | null>(null);
  const cleanup = canCleanupRole(entry);
  const expectedProfile = entry.record.intent.anchor.admin_profile_id;
  const choices = profiles.filter((profile) => Number.isSafeInteger(profile.id) && profile.id > 0 && profile.kind === "username_password");
  const [selected, setSelected] = useState(() =>
    choices.some((profile) => profile.id === expectedProfile) ? String(expectedProfile) : "",
  );
  const matches = selected === String(expectedProfile) && choices.some((profile) => String(profile.id) === selected);
  const evidence = JSON.stringify([entry, selected, choices.find((profile) => String(profile.id) === selected)]);
  const observed = useRef({ evidence, owner: {} });
  if (observed.current.evidence !== evidence) observed.current = { evidence, owner: {} };
  const owner = observed.current.owner;
  const acknowledged = acknowledgement === owner;
  const confirmedRoleName = confirmation?.owner === owner ? confirmation.text : "";
  const permitted = open && !disabled && matches && acknowledged && (!cleanup || confirmedRoleName === entry.record.intent.role_name);
  const submissionIdentity = JSON.stringify([evidence, open, disabled, acknowledged, confirmedRoleName]);
  const currentSubmission = useRef({ identity: submissionIdentity, permitted });
  if (currentSubmission.current.identity !== submissionIdentity) currentSubmission.current = { identity: submissionIdentity, permitted };
  const submission = currentSubmission.current;
  const mounted = useRef(false);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const invalidateSubmission = () => {
    currentSubmission.current = { identity: "", permitted: false };
  };
  const submit = () => {
    if (!mounted.current || currentSubmission.current !== submission || !submission.permitted) return;
    invalidateSubmission();
    if (cleanup) void onConfirm(entry, Number(selected), confirmedRoleName);
    else void onConfirm(entry, Number(selected));
  };
  return (
    <div className="mt-3 grid min-w-0 gap-2">
      <Button
        type="button"
        variant="outline"
        className="h-8 w-8 px-0"
        title={cleanup ? "Clean up remote role" : "Verify role presence"}
        aria-label={`${cleanup ? "Clean up remote role" : "Verify role presence"} for ${entry.record.intent.role_name}`}
        disabled={disabled || choices.length === 0}
        onClick={() => {
          invalidateSubmission();
          setAcknowledgement(null);
          setConfirmation(null);
          setOpen((value) => !value);
        }}
      >
        {cleanup ? <Trash2 className="h-4 w-4" /> : <ShieldCheck className="h-4 w-4" />}
      </Button>
      {open ? (
        <div className="grid min-w-0 gap-2">
          <Notice tone="warn">Verify only the original cluster. A clone, restore or replacement invalidates this evidence.</Notice>
          {cleanup ? (
            <Notice tone="warn">
              Remote role cleanup reassigns owned objects, revokes managed privileges and drops the role. Local credentials are retained.
            </Notice>
          ) : null}
          <label className="grid min-w-0 gap-1">
            Admin profile
            <select
              aria-label="Reconciliation admin profile"
              className="h-9 w-full rounded border border-stone-300 bg-white px-2"
              value={selected}
              disabled={disabled}
              onChange={(event) => {
                invalidateSubmission();
                setSelected(event.target.value);
                setAcknowledgement(null);
                setConfirmation(null);
              }}
            >
              <option value="">Select profile</option>
              {choices.map((profile) => (
                <option key={profile.id} value={String(profile.id)}>
                  {profile.label || `Profile ${profile.id}`}
                </option>
              ))}
            </select>
          </label>
          {!matches ? <Notice tone="warn">The selected profile does not match recorded admin profile {expectedProfile}.</Notice> : null}
          <label className="flex items-start gap-2">
            <input
              type="checkbox"
              checked={acknowledged}
              disabled={disabled || !matches}
              onChange={(event) => {
                invalidateSubmission();
                setAcknowledgement(event.target.checked ? owner : null);
              }}
            />
            <span>The original cluster has not been cloned, restored or replaced.</span>
          </label>
          {cleanup ? (
            <label className="grid min-w-0 gap-1">
              Role name
              <Input
                aria-label="Confirm remote role name"
                value={confirmedRoleName}
                disabled={disabled || !matches}
                onChange={(event) => {
                  invalidateSubmission();
                  setConfirmation({ owner, text: event.target.value });
                }}
              />
            </label>
          ) : null}
          <Button type="button" variant={cleanup ? "danger" : "outline"} disabled={!permitted} onClick={submit}>
            {cleanup ? "Confirm remote cleanup" : "Confirm role presence"}
          </Button>
        </div>
      ) : null}
    </div>
  );
}
