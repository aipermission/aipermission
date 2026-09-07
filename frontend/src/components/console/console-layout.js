export function consoleShellGridClass(targetsCompact, tokensCompact) {
  if (targetsCompact && tokensCompact) return "grid-cols-[56px_minmax(0,1fr)_56px]";
  if (targetsCompact) return "grid-cols-[56px_minmax(0,1fr)_360px]";
  if (tokensCompact) return "grid-cols-[360px_minmax(0,1fr)_56px]";
  return "grid-cols-[360px_minmax(0,1fr)_360px]";
}
