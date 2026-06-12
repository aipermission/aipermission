export function ConnectorRuleButton({ active, children, className = "", ...props }) {
  return (
    <button
      type="button"
      className={`h-8 rounded-md border px-2 text-xs font-semibold transition disabled:pointer-events-none disabled:opacity-50 ${
        active ? "permission-button-active border-emerald-900 bg-emerald-950 text-white" : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"
      } ${className}`}
      {...props}
    >
      {children}
    </button>
  );
}
