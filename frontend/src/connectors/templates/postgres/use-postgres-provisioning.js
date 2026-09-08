import { useEffect, useEffectEvent, useMemo, useState } from "react";
import { apiPost } from "../../../lib/api";
import { errorMessage } from "../../../lib/errors";
import { useRequestGuard } from "../../../lib/request-guard";
import { requireCompletedConnectorAction } from "../_shared/action-result";
import {
  buildProvisionScope,
  buildProvisionSQLPreview,
  defaultProvisionForm,
  groupMetadataRows,
  metadataSQL,
  readableScopeSummary,
} from "./provisioning";

const emptyMetadata = { state: "idle", error: "", schemas: [] };
const emptyScope = { all_schemas: true, schemas: {} };
const emptyState = { state: "idle", error: "", result: null };

export function usePostgresProvisioning({ value, onOperationComplete }) {
  const [form, setForm] = useState(defaultProvisionForm);
  const [metadata, setMetadata] = useState(emptyMetadata);
  const [scope, setScope] = useState(emptyScope);
  const [state, setState] = useState(emptyState);
  const targetRef = value.profile?.ref || "";
  const targetID = value.target?.id;
  const profileID = value.profile?.id;
  const requestGuard = useRequestGuard(`${value.open ? "open" : "closed"}:${targetID || 0}:${profileID || 0}:${targetRef}`);
  const loadMetadataForEffect = useEffectEvent(() => loadMetadata());
  const selectedScope = useMemo(() => buildProvisionScope(scope), [scope]);
  const sqlPreview = useMemo(
    () =>
      buildProvisionSQLPreview({
        roleName: form.role_name,
        preset: form.preset,
        database: value.target?.config?.database || "database",
        scope: selectedScope,
      }),
    [form.role_name, form.preset, selectedScope, value.target?.config?.database],
  );

  useEffect(() => {
    if (!value.open) return;
    setForm(defaultProvisionForm);
    setMetadata(emptyMetadata);
    setScope(emptyScope);
    setState(emptyState);
    if (targetRef) void loadMetadataForEffect();
  }, [value.open, targetRef, targetID, profileID]);

  async function loadMetadata() {
    if (!targetRef) return;
    const request = requestGuard.begin("metadata");
    setMetadata({ state: "loading", error: "", schemas: [] });
    try {
      const response = await apiPost(
        "/api/connector-actions/local-run",
        {
          target_ref: targetRef,
          action_name: "query_readonly",
          input: { sql: metadataSQL, max_rows: 1000 },
          reason: "load Postgres schema metadata for managed credential provisioning",
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      const item = requireCompletedConnectorAction(response, "Could not load schema metadata.");
      if (!item) {
        setMetadata({ state: "pending", error: "Metadata request is awaiting approval.", schemas: [] });
        return;
      }
      setMetadata({ state: "ready", error: "", schemas: groupMetadataRows(item.output?.rows || []) });
    } catch (error) {
      if (request.isCurrent()) setMetadata({ state: "error", error: errorMessage(error, "Could not load schema metadata."), schemas: [] });
    } finally {
      request.complete();
    }
  }

  async function provisionUser(event) {
    event.preventDefault();
    if (!canSubmit(targetID, profileID, form, selectedScope)) return;
    const request = requestGuard.begin("provision");
    setState({ state: "running", error: "", result: null });
    try {
      const result = await apiPost(
        `/api/connector-targets/${targetID}/profiles/${profileID}/provision`,
        {
          input: {
            role_name: form.role_name,
            profile_label: form.profile_label || form.role_name,
            preset: form.preset,
            scope: selectedScope,
          },
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      setState({ state: "ready", error: "", result });
      try {
        await onOperationComplete?.({ message: "Managed Postgres credential created." }, value);
      } catch (error) {
        if (request.isCurrent()) {
          setState({ state: "ready", error: `Credential created, but connector refresh failed: ${errorMessage(error)}`, result });
        }
      }
    } catch (error) {
      if (request.isCurrent()) {
        setState({ state: "error", error: errorMessage(error, "Could not create managed Postgres credential."), result: null });
      }
    } finally {
      request.complete();
    }
  }

  return {
    form,
    updateForm: (field, nextValue) => setForm((current) => ({ ...current, [field]: nextValue })),
    metadata,
    scope,
    setScope,
    state,
    targetRef,
    selectedScope,
    sqlPreview,
    scopeSummary: readableScopeSummary(selectedScope, form.preset),
    canSubmit: canSubmit(targetID, profileID, form, selectedScope),
    loadMetadata,
    provisionUser,
  };
}

function canSubmit(targetID, profileID, form, selectedScope) {
  return Boolean(targetID && profileID && form.role_name.trim() && selectedScope);
}
