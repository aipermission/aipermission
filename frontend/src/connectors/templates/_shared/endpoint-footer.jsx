export function ConnectorEndpointFooter({
  leading,
  trailing,
  borderClass = "",
  mutedClass = "",
  className = "",
  leadingClassName = "font-mono",
}) {
  return (
    <div className={`flex min-w-0 items-center justify-between gap-3 text-xs ${borderClass} ${mutedClass} ${className}`}>
      <span className={`truncate ${leadingClassName}`}>{leading}</span>
      <span className="flex min-w-0 items-center gap-2 truncate">{trailing}</span>
    </div>
  );
}
