import { RedactionSettingsCard, SecurityToggleCard } from "../components/security/security-settings-panels";
import { Notice } from "../components/ui/notice";
import { useSecurityPageState } from "./use-security-page-state";

const securityToggleSettings = ["reusable_tokens", "expose_mcp_server_metadata", "mcp_start_enabled"];

export function SecurityPage() {
  const state = useSecurityPageState();
  const toggleDisabled = state.security.state === "loading" || state.securityAction.state === "saving";
  return (
    <section className="mx-auto grid w-full max-w-2xl gap-5">
      <div>
        <h1 className="text-lg font-semibold">Security</h1>
        <p className="text-sm text-stone-500">Control token copy behavior, MCP metadata exposure, and redaction rules.</p>
      </div>
      <SecurityNotices state={state} />
      {securityToggleSettings.map((setting) => (
        <SecurityToggleCard
          key={setting}
          setting={setting}
          value={state.security.data?.[setting]}
          disabled={toggleDisabled}
          onUpdate={state.updateSecurity}
        />
      ))}
      <RedactionSettingsCard
        security={state.security}
        action={state.redactionAction}
        rules={state.redactionRules}
        form={state.redactionForm}
        onUpdateSecurity={state.updateSecurity}
        onUpdateForm={state.updateRedactionForm}
        onCreate={state.createRedactionRule}
        onToggle={state.toggleRedactionRule}
        onDelete={state.deleteRedactionRule}
      />
    </section>
  );
}

function SecurityNotices({ state }) {
  return (
    <>
      {state.security.state === "error" ? <Notice tone="bad">{state.security.error}</Notice> : null}
      {state.securityAction.message ? <Notice tone="good">{state.securityAction.message}</Notice> : null}
      {state.securityAction.state === "error" ? <Notice tone="bad">{state.securityAction.error}</Notice> : null}
      {state.redactionAction.message ? <Notice tone="good">{state.redactionAction.message}</Notice> : null}
      {state.redactionAction.state === "error" ? <Notice tone="bad">{state.redactionAction.error}</Notice> : null}
    </>
  );
}
