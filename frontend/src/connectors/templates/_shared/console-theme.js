export function connectorConsoleTheme(theme) {
  const light = theme === "light";
  return {
    panel: light ? "bg-white text-stone-900" : "bg-[#1e1e1e] text-stone-100",
    muted: light ? "text-stone-500" : "text-stone-400",
    border: light ? "border-stone-200" : "border-stone-700",
    subtlePanel: light ? "bg-stone-50" : "bg-[#252526]",
    input: light
      ? "border-stone-300 bg-white text-stone-900 placeholder:text-stone-400"
      : "border-stone-700 bg-[#1a1a1a] text-stone-100 placeholder:text-stone-500",
    rowHover: light ? "hover:bg-stone-50" : "hover:bg-stone-800/60",
    activeRow: light ? "border-emerald-200 bg-emerald-50 text-emerald-950" : "border-emerald-700 bg-emerald-950/40 text-emerald-100",
  };
}
