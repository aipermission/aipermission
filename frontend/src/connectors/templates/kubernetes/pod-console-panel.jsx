import { LiveConsolePanel } from "../_shared/live-console-panel";

export function KubernetesPodConsolePanel({
  children,
  pod,
  selectedRuntimeTarget,
  sessionLive,
  pending,
  theme,
  mutedClass,
  borderClass,
  onStart,
  onEnd,
}) {
  const podRef = pod ? `${pod.namespace}/${pod.name}` : "";
  return (
    <LiveConsolePanel
      subject={pod ? "pod" : ""}
      subjectRef={podRef}
      emptyMessage="Select a pod, then open a live console inside it."
      selectedRuntimeTarget={selectedRuntimeTarget}
      sessionLive={sessionLive}
      pending={pending}
      theme={theme}
      mutedClass={mutedClass}
      borderClass={borderClass}
      onStart={onStart}
      onEnd={onEnd}
    >
      {children}
    </LiveConsolePanel>
  );
}

export function kubernetesConsoleSessionName(target, pod) {
  return `kubernetes:${target?.ref || "target"}:${pod?.namespace || "namespace"}:${pod?.name || "pod"}`;
}
