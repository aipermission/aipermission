import { useEffect, useState } from "react";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";

const emptyActionState = { state: "idle", error: null, message: null };
const defaultSecurity = {
  reusable_tokens: false,
  expose_mcp_server_metadata: false,
  mcp_start_enabled: false,
  redaction_mode: "basic",
};

export function useSecurityPageState() {
  const [security, setSecurity] = useState({ state: "loading", data: defaultSecurity, error: null });
  const { actionState: securityAction, runAction: runSecurityAction } = useAsyncAction(emptyActionState);
  const [redactionRules, setRedactionRules] = useState({ state: "loading", data: [], error: null });
  const { actionState: redactionAction, runAction: runRedactionAction } = useAsyncAction(emptyActionState);
  const [redactionForm, setRedactionForm] = useState({ name: "", pattern: "", enabled: true });

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

  async function updateSecurity(patch, message) {
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
