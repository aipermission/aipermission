import { useEffect, useRef, useState } from "react";
import type { FormEvent } from "react";
import { apiDelete, apiGet, apiPost, apiPut } from "../lib/api";
import { useAsyncAction } from "../lib/use-async-action";
import { errorMessage } from "../lib/errors";
import {
  securitySettingsResponse,
  redactionRulesResponse,
  type RedactionForm,
  type RedactionRule,
  type SecuritySettings,
  type SecuritySettingsUpdate,
} from "../lib/gateway-contracts/security-settings-contract";

const emptyActionState = { state: "idle", error: null, message: null };
const defaultSecurity: SecuritySettings = {
  reusable_tokens: false,
  expose_mcp_server_metadata: false,
  mcp_start_enabled: false,
  redaction_mode: "basic",
  revision: "",
};

export function useSecurityPageState() {
  const [security, setSecurity] = useState<{ state: string; data: SecuritySettings; error: string | null }>({
    state: "loading",
    data: defaultSecurity,
    error: null,
  });
  const securitySavingRef = useRef(false);
  const { actionState: securityAction, runAction: runSecurityAction } = useAsyncAction(emptyActionState);
  const [redactionRules, setRedactionRules] = useState<{ state: string; data: RedactionRule[]; error: string | null }>({
    state: "loading",
    data: [],
    error: null,
  });
  const { actionState: redactionAction, runAction: runRedactionAction } = useAsyncAction(emptyActionState);
  const [redactionForm, setRedactionForm] = useState({ name: "", pattern: "", enabled: true });

  async function loadSecurity() {
    try {
      const data = securitySettingsResponse(await apiGet("/api/settings/security"));
      setSecurity({ state: "ready", data, error: null });
    } catch (error) {
      setSecurity({ state: "error", data: defaultSecurity, error: errorMessage(error, "Could not load security settings.") });
    }
  }

  async function loadRedactionRules() {
    try {
      const data = redactionRulesResponse(await apiGet("/api/settings/redaction-rules"));
      setRedactionRules({ state: "ready", data, error: null });
    } catch (error) {
      setRedactionRules({ state: "error", data: [], error: errorMessage(error, "Could not load redaction rules.") });
    }
  }

  useEffect(() => {
    void loadSecurity();
    void loadRedactionRules();
  }, []);

  async function updateSecurity(patch: Partial<Omit<SecuritySettings, "revision">>, message: string) {
    if (security.state !== "ready" || !security.data.revision || securitySavingRef.current) return;
    const nextData = { ...security.data, ...patch };
    const request = {
      reusable_tokens: nextData.reusable_tokens,
      expose_mcp_server_metadata: nextData.expose_mcp_server_metadata,
      mcp_start_enabled: nextData.mcp_start_enabled,
      redaction_mode: nextData.redaction_mode,
      expected_revision: security.data.revision,
    } satisfies SecuritySettingsUpdate;
    securitySavingRef.current = true;
    await runSecurityAction({
      pending: "saving",
      successMessage: message,
      action: async () => {
        try {
          const data = securitySettingsResponse(await apiPut("/api/settings/security", request));
          setSecurity({ state: "ready", data, error: null });
        } catch (error) {
          if (error && typeof error === "object" && "status" in error && error.status === 409) await loadSecurity();
          throw error;
        } finally {
          securitySavingRef.current = false;
        }
      },
    });
  }

  function updateRedactionForm<Key extends keyof RedactionForm>(field: Key, value: RedactionForm[Key]) {
    setRedactionForm((current) => ({ ...current, [field]: value }));
  }

  async function createRedactionRule(event: FormEvent<HTMLFormElement>) {
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

  async function toggleRedactionRule(rule: RedactionRule, enabled: boolean) {
    await runRedactionAction({
      pending: "saving",
      successMessage: enabled ? "Custom rule enabled." : "Custom rule disabled.",
      action: async () => {
        await apiPut(`/api/settings/redaction-rules/${rule.id}`, { name: rule.name, pattern: rule.pattern, enabled });
        await loadRedactionRules();
      },
    });
  }

  async function deleteRedactionRule(rule: RedactionRule) {
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
