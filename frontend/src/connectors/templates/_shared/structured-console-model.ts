export function createStructuredConsoleModel(defaultProfileLabel: string) {
  return {
    targetProfileLabel: ({ target }: { target?: { profile_label?: string } | null }) => target?.profile_label || defaultProfileLabel,
    usesLiveConsole: () => false,
    recoverableRunningActions: (): string[] => [],
  };
}
