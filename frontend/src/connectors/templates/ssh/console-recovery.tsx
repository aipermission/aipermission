import { captureConsoleSessionRecovery } from "../_shared/console-recovery";
import { isHostKeyError } from "./model-helpers";
import { SSHConnectorOperationsLoader } from "./operations-loader";
import type { SSHOperation } from "./operation-types";
import type { ConsoleRecoveryContext } from "../_shared/console-recovery-types";
import type { ConsoleRuntimeTarget } from "../../../components/use-gateway-resources";

export const sshConsoleRecovery = captureConsoleSessionRecovery<SSHOperation>({
  kind: "ssh",
  recover(error, context) {
    if (context.operation !== "new-session" || !isHostKeyError(error) || !isSSHRuntimeTarget(context.target)) return null;
    return {
      runtimeTarget: context.target,
      operation: {
        open: true,
        connector_kind: "ssh",
        type: "host-key",
        hostKey: error.data.host_key,
        action: { kind: "ssh", type: "new-session" },
        state: "idle",
        error: null,
      },
    };
  },
  render({ value, onChange, onOperationComplete }) {
    return <SSHConnectorOperationsLoader value={value} credentials={[]} onChange={onChange} onOperationComplete={onOperationComplete} />;
  },
});

function isSSHRuntimeTarget(target: ConsoleRecoveryContext["target"]): target is ConsoleRuntimeTarget {
  return Number.isSafeInteger(target.id) && target.id > 0 && target.connector_kind === "ssh" && typeof target.name === "string";
}
