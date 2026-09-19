import { useEffect, useRef, useState } from "react";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";

const emptyActionState = { state: "idle", error: null, message: null };
const defaultSecurity = {
  reusable_tokens: false,
  expose_mcp_server_metadata: false,
  mcp_start_enabled: false,
  redaction_mode: "basic",
  revision: "",
};

function requireSecurityDocument(data) {
  const validMode = data?.redaction_mode === "basic" || data?.redaction_mode === "off";
  const validBooleans = ["reusable_tokens", "expose_mcp_server_metadata", "mcp_start_enabled"].every(
    (field) => typeof data?.[field] === "boolean",
  );
  if (!validMode || !validBooleans || typeof data?.revision !== "string" || data.revision.trim() === "") {
    throw new Error("Security settings response is invalid.");
  }
  return data;
}

export function useSecurityPageState() {
  const [security, setSecurity] = useState({ state: "loading", data: defaultSecurity, error: null });
  const securitySavingRef = useRef(false);
  const { actionState: securityAction, runAction: runSecurityAction } = useAsyncAction(emptyActionState);
  const [redactionRules, setRedactionRules] = useState({ state: "loading", data: [], error: null });
  const { actionState: redactionAction, runAction: runRedactionAction } = useAsyncAction(emptyActionState);
  const [redactionForm, setRedactionForm] = useState({ name: "", pattern: "", enabled: true });

  async function loadSecurity() {
    try {
      const data = requireSecurityDocument(await apiGet("/api/settings/security"));
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

  async function updateSecurity(patch, message) {
    if (security.state !== "ready" || !security.data.revision || securitySavingRef.current) return;
    const nextData = { ...security.data, ...patch };
    const request = {
      reusable_tokens: nextData.reusable_tokens,
      expose_mcp_server_metadata: nextData.expose_mcp_server_metadata,
      mcp_start_enabled: nextData.mcp_start_enabled,
      redaction_mode: nextData.redaction_mode,
      expected_revision: security.data.revision,
    };
    securitySavingRef.current = true;
    await runSecurityAction({
      pending: "saving",
      successMessage: message,
      action: async () => {
        try {
          const data = requireSecurityDocument(await apiPut("/api/settings/security", request));
          setSecurity({ state: "ready", data, error: null });
        } catch (error) {
          if (error?.status === 409) await loadSecurity();
          throw error;
        } finally {
          securitySavingRef.current = false;
        }
      },
    });
  }

  function updateRedactionForm(field, value) {
    setRedactionForm((current) => ({ ...current, [field]: value }));
  }

  async function createRedactionRule(event) {
    event.preventDefault();
    await runRedactionAction({
      pending: "saving",
      successMessage: "Custom redaction rule added.",
      action: async () => {
        await apiPost("/api/settings/redaction-rules", redactionForm);
        setRedactionForm({ name: "", pattern: "", enabled: true });
        await loadRedactionRules();
      },
    });
  }

  async function toggleRedactionRule(rule, enabled) {
    await runRedactionAction({
      pending: "saving",
      successMessage: enabled ? "Custom rule enabled." : "Custom rule disabled.",
      action: async () => {
        await apiPut(`/api/settings/redaction-rules/${rule.id}`, { name: rule.name, pattern: rule.pattern, enabled });
        await loadRedactionRules();
      },
    });
  }

  async function deleteRedactionRule(rule) {
    await runRedactionAction({
      pending: "deleting",
      successMessage: "Custom redaction rule deleted.",
      action: async () => {
        await apiDelete(`/api/settings/redaction-rules/${rule.id}`);
        await loadRedactionRules();
      },
    });
  }

  return {
    security,
    securityAction,
    redactionRules,
    redactionAction,
    redactionForm,
    updateSecurity,
    updateRedactionForm,
    createRedactionRule,
    toggleRedactionRule,
    deleteRedactionRule,
  };
}
