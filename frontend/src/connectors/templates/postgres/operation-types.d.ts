import type { components } from "../../../../types/generated-openapi";
import type { DatabaseTarget } from "../_shared/database-model-types";

export type PostgresOperation = {
  open: boolean;
  connector_kind?: "postgres" | "";
  type?: "provision-user" | "backup-restore" | "role-history" | "";
  state?: string;
  error?: string | null;
  target?: Partial<Pick<DatabaseTarget, "id" | "name" | "config" | "profiles">>;
  profile?: Partial<Pick<components["schemas"]["ConnectorCredentialProfile"], "id" | "ref" | "label">>;
};
export type ProvisionResult = { profile?: { id?: number; label?: string }; result?: { display_text?: string } };
export type ProvisionOperationProps = {
  value: PostgresOperation;
  onOperationComplete?: (_result: { message: string }, _operation: PostgresOperation) => unknown;
};
