export type SSHConsoleRuntime = {
  id: number;
  connector_kind: string;
  name?: string;
  username?: string;
  host?: string;
  port?: string | number;
  target?: { transfer_runtime_id?: number };
};
export type SSHToolbarProps = {
  theme: "light" | "dark";
  selectedRuntimeTarget: SSHConsoleRuntime | null;
  selectedSession: { status?: string } | null;
  selectedSessionLive: boolean;
  selectedUnreadMessages?: readonly unknown[];
  onOpenMessages?: () => void;
  onRefreshSessions?: () => void | Promise<unknown>;
  onNewSession?: () => void | Promise<unknown>;
  onEndSession?: () => void | Promise<unknown>;
  onInterrupt?: () => void | Promise<unknown>;
  liveConsoleTargets?: SSHConsoleRuntime[];
};
