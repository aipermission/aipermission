export function consoleShellGridClass(targetsCompact, tokensCompact) {
  if (targetsCompact && tokensCompact) return "2xl:grid-cols-[56px_minmax(0,1fr)_56px]";
  if (targetsCompact) return "2xl:grid-cols-[56px_minmax(0,1fr)_clamp(240px,20vw,360px)]";
  if (tokensCompact) return "2xl:grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_56px]";
  return "2xl:grid-cols-[clamp(240px,20vw,360px)_minmax(0,1fr)_clamp(240px,20vw,360px)]";
}
