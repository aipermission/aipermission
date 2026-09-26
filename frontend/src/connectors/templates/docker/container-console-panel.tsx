import { LiveConsolePanel, type LiveConsolePanelProps } from "../_shared/live-console-panel";
import type { ConsoleSession } from "../../../lib/gateway-contracts/security-contracts";
import type { DockerResource } from "./resource-types";

type DockerContainerConsolePanelProps = Omit<LiveConsolePanelProps, "subject" | "subjectRef" | "emptyMessage" | "warning"> & {
  target?: { ref?: string } | null;
  container: DockerResource | null;
  containerRef: string;
  session?: Pick<ConsoleSession, "id" | "name"> | null;
};

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
}: DockerContainerConsolePanelProps) {
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

export function dockerConsoleSessionName(target: { ref?: string } | null | undefined, containerRef: string) {
  return `docker:${target?.ref || "target"}:${containerRef}`;
}
