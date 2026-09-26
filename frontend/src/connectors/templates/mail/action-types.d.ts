import type { ConnectorActionResponse } from "../../../lib/gateway-contracts/security-contracts";
import type { MailActionResult } from "./action-result-dialog";
import type { MailSubmittedFields, MailMessageRef } from "./message-types";

export type MailActionItem = Pick<ConnectorActionResponse, "status" | "action_name"> & Partial<ConnectorActionResponse> & { id?: number | string };
export interface MailPendingContext {
  preferredFolder?: string;
  subject?: string;
  folder?: string;
  reset?: boolean;
  cursor?: string;
  unread?: boolean;
  messageKey?: string;
  wasRead?: boolean;
  fields?: MailSubmittedFields;
  reply?: boolean;
  messageRef?: MailMessageRef;
  draftFingerprint?: string;
}
export interface MailPendingAction {
  requestID: number;
  actionName: string;
  context: MailPendingContext;
  generation: number;
  scope: string;
}
export interface MailActionResolution {
  state: "pending" | "completed" | "failed";
  item: MailActionItem;
}
export interface MailRunnerState {
  state: string;
  error: string;
  message: string;
  result?: MailActionResult | null;
}
export type RunMailAction = (_action: string, _input: Record<string, unknown>, _reason: string, _busy?: string, _context?: MailPendingContext) => Promise<ConnectorActionResponse | null>;
