import type { ConnectorApproval } from "../../../lib/gateway-contracts/security-contracts";
import type { KafkaTarget } from "./form-types";

export type KafkaView = "topics" | "groups";
export interface KafkaResource {
  name: string;
  partition_count?: number;
  replication_factor?: number;
  state?: string;
  protocol_type?: string;
}
export interface KafkaPartition {
  partition: number;
  topic?: string;
  error?: string;
  committed_offset?: string;
  end_offset?: string;
  earliest_offset?: string;
}
export type KafkaDetail = Record<string, unknown> & { partitions?: KafkaPartition[]; members?: unknown[] };
export interface KafkaBrowserProps {
  target: { ref: string; config?: KafkaTarget["config"] };
  approvals?: { data?: ConnectorApproval[] };
  session?: { active: boolean; startedAt?: string } | null;
  onRefreshActivity?: () => unknown;
}
export interface KafkaPublishForm {
  partition: string;
  key: string;
  key_encoding: string;
  value: string;
  value_encoding: string;
  headers: string;
}
export interface KafkaOffsetForm {
  selection: string;
  offset: string;
}
export interface KafkaWriteDialog<Form> {
  open: boolean;
  form: Form;
  error: string;
}
