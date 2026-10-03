import type { ButtonHTMLAttributes } from "react";
import type { ExecutionRule } from "../../lib/gateway-contracts/security-contracts";

type ConnectorRuleButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { active: boolean };

export function ConnectorRuleButton({ active, children, className = "", title, ...props }: ConnectorRuleButtonProps) {
  const label = typeof children === "string" ? children : "";
  return (
    <button
      type="button"
      title={title || label || undefined}
      className={`h-7 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap rounded-md border px-1 text-[10px] font-semibold leading-none transition disabled:pointer-events-none disabled:opacity-50 ${
        active
          ? "permission-button-active border-emerald-900 bg-emerald-950 text-white"
          : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"
      } ${className}`}
      {...props}
    >
      {children}
    </button>
  );
}

const rules: readonly { value: ExecutionRule | ""; label: string }[] = [
  { value: "", label: "Disabled" },
  { value: "blocked", label: "Blocked" },
  { value: "approval_required", label: "Prompt" },
  { value: "always_run", label: "Always" },
];

export function ConnectorRuleButtons({
  rule,
  saving = false,
  disabled = false,
  onSetRule,
}: {
  rule: string;
  saving?: boolean;
  disabled?: boolean;
  onSetRule: (_rule: ExecutionRule | "") => unknown;
}) {
  return (
    <div className="grid grid-cols-4 gap-1">
      {rules.map((choice) => (
        <ConnectorRuleButton
          key={choice.value}
          active={rule === choice.value && !disabled}
          disabled={saving || disabled}
          onClick={() => onSetRule(choice.value)}
        >
          {choice.label}
        </ConnectorRuleButton>
      ))}
    </div>
  );
}
