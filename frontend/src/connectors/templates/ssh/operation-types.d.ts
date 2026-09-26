import type { Dispatch, SetStateAction } from "react";
import type { SSHCredentialResource, SSHDockerContainer, SSHHostKey, SSHHostKeyAction, SSHModelTarget } from "./model-types";
import type { SSHProfile } from "./form-types";
import type { resumeHostKeyAction } from "./model";

export type SSHDockerResponse = {
  ok: boolean;
  available?: boolean;
  containers?: SSHDockerContainer[];
  stdout?: string;
  stderr?: string;
  exit_code?: number;
  duration_ms?: number;
};
export type SSHOperation = {
  open: boolean;
  connector_kind?: string;
  type?: string;
  state?: string;
  error?: string | null;
  target?: SSHModelTarget | null;
  profile?: SSHProfile | null;
  container?: SSHDockerContainer | null;
  data?: SSHDockerResponse | null;
  hostKey?: SSHHostKey;
  action?: SSHHostKeyAction;
};
export type SSHOperationProps = {
  value: SSHOperation | null;
  credentials: SSHCredentialResource[];
  onChange: Dispatch<SetStateAction<SSHOperation>>;
  onOperationComplete?: (
    _result: Awaited<ReturnType<typeof resumeHostKeyAction>>,
    _operation: SSHHostKeyAction & { connector_kind: string },
  ) => void | Promise<void>;
};
export type OperationDialogProps = { value: SSHOperation; onClose: () => void };
export type ReadDockerLogs = (
  _target?: SSHModelTarget | null,
  _container?: SSHDockerContainer | null,
  _tail?: number,
  _profile?: SSHProfile | null,
) => Promise<void>;
