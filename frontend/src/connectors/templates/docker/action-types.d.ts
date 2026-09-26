import type { GuardedConnectorActionOptions } from "../_shared/action-runner";

export type DockerRunActionOptions = Pick<GuardedConnectorActionOptions, "actionName" | "input" | "reason" | "busy" | "channel"> & {
  showResult?: boolean;
};
