import type { connectorConsoleTheme } from "../_shared/console-theme";

export type QueueCounters = {
  messages_ready?: unknown;
  messages_unacknowledged?: unknown;
  messages?: unknown;
  consumers?: unknown;
  durable?: unknown;
};
export type RabbitQueue = QueueCounters & { name: string; vhost?: string; state?: string; [field: string]: unknown };
export type RabbitMessage = {
  payload?: unknown;
  payload_encoding?: unknown;
  redelivered?: unknown;
  properties?: unknown;
  [field: string]: unknown;
};
export type RabbitStyles = ReturnType<typeof connectorConsoleTheme>;
export type RabbitActivity = {
  id?: number;
  request_id?: number;
  target_ref?: string;
  action_name?: string;
  status?: string;
  display_text?: string;
  output?: unknown;
};
export type RabbitTarget = {
  ref: string;
  config?: { vhost?: string; scheme?: string; host?: string; port?: string | number };
};
export type RabbitSession = { active: boolean; startedAt?: string };
export type RabbitBrowserProps = {
  target: RabbitTarget;
  approvals?: { state: string; data?: RabbitActivity[] | null };
  session?: RabbitSession | null;
  onRefreshActivity?: () => unknown;
};
