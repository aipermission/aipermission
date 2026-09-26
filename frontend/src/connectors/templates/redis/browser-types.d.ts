import type { Dispatch, SetStateAction } from "react";
import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { ConnectorActionState, GuardedConnectorActionOptions } from "../_shared/action-runner";
import type { connectorConsoleTheme } from "../_shared/console-theme";

export type RedisKeyResult = {
  key: string;
  type: string;
  value?: unknown;
  ttl_ms?: number;
  truncated?: boolean;
  [field: string]: unknown;
};
export type RedisActionOptions = Pick<GuardedConnectorActionOptions, "actionName" | "reason" | "input" | "busy" | "channel">;
export type RedisRunAction = (_options: RedisActionOptions) => Promise<ConnectorActionResponse | null>;
export type RedisConfirmState = {
  open: boolean;
  type: string;
  title: string;
  description: string;
  details: { label: string; value: string }[];
  tone: "warn" | "bad";
  pending: boolean;
  error: string;
  onConfirm: ((_isCurrent: () => boolean) => Promise<boolean>) | null;
};
export type RedisBrowserProps = {
  target: {
    ref: string;
    connector_kind?: string;
    config?: { server_family?: string; host?: string; port?: string | number; database?: string | number; connection_mode?: string };
  };
  approvals?: { data?: { target_ref?: string; display_text?: string; status?: string; action_name?: string }[] };
  session?: { active: boolean; startedAt?: string } | null;
  onRefreshActivity?: () => unknown;
};
export type RedisStyles = ReturnType<typeof connectorConsoleTheme>;
export type RedisMutationOptions = {
  resetKey: string;
  product: string;
  activeKey: string;
  keyResult: RedisKeyResult | null;
  valueDraft: string;
  newKey: string;
  newValue: string;
  ttlDraft: string;
  selectedKeys: string[];
  setState: Dispatch<SetStateAction<ConnectorActionState>>;
  setNewKey: Dispatch<SetStateAction<string>>;
  setNewValue: Dispatch<SetStateAction<string>>;
  setKeys: Dispatch<SetStateAction<string[]>>;
  setSelectedKeys: Dispatch<SetStateAction<string[]>>;
  setActiveKey: Dispatch<SetStateAction<string>>;
  setKeyResult: Dispatch<SetStateAction<RedisKeyResult | null>>;
  setValueDraft: Dispatch<SetStateAction<string>>;
  runAction: RedisRunAction;
  loadKey: (_key: string) => Promise<void>;
};
