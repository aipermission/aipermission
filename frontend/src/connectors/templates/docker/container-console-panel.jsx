import { LiveConsolePanel } from "../_shared/live-console-panel";

export function DockerContainerConsolePanel({
  children,
  target,
  container,
  containerRef,
  selectedRuntimeTarget,
  session,
  sessionLive,
  pending,
  theme,
  mutedClass,
  borderClass,
  onStart,
  onEnd,
}) {
  const lastSessionForOtherContainer = session?.id && session?.name !== dockerConsoleSessionName(target, containerRef);
  return (
    <LiveConsolePanel
      subject={container ? "container" : ""}
      subjectRef={containerRef}
      emptyMessage="Select a container, then open a live console inside it."
      selectedRuntimeTarget={selectedRuntimeTarget}
      sessionLive={sessionLive}
      pending={pending}
      theme={theme}
      mutedClass={mutedClass}
      borderClass={borderClass}
      warning={lastSessionForOtherContainer ? "Starting this console will close the current Docker console session for this profile." : ""}
      onStart={onStart}
      onEnd={onEnd}
    >
      {children}
    </LiveConsolePanel>
  );
}

export function dockerConsoleSessionName(target, containerRef) {
  return `docker:${target?.ref || "target"}:${containerRef}`;
}
