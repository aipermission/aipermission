export function consoleShellGridClass(targetsCompact, tokensCompact) {
  if (targetsCompact && tokensCompact) return "grid-cols-[56px_minmax(0,1fr)_56px]";
  if (targetsCompact) return "grid-cols-[56px_minmax(0,1fr)_clamp(240px,20vw,360px)]";
  if (tokensCompact) return "grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_56px]";
  return "grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_clamp(240px,20vw,360px)]";
}
