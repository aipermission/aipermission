export function ConnectorRuleButton({ active, children, className = "", title, ...props }) {
  const label = typeof children === "string" ? children : "";
  return (
    <button
      type="button"
      title={title || label || undefined}
      className={`h-7 min-w-0 overflow-hidden text-ellipsis whitespace-nowrap rounded-md border px-1 text-[10px] font-semibold leading-none transition disabled:pointer-events-none disabled:opacity-50 ${
        active ? "permission-button-active border-emerald-900 bg-emerald-950 text-white" : "border-stone-300 bg-white text-stone-700 hover:bg-stone-100"
      } ${className}`}
      {...props}
    >
      {children}
    </button>
  );
}
