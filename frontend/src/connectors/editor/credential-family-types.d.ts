import type { ComponentType, ReactNode } from "react";
import type { InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { CredentialResource } from "../../lib/gateway-contracts/core-resource-contracts";
import type { AsyncActionState } from "../../lib/use-async-action";
import type { CredentialDisplayRow } from "./credential-row";
import type { CredentialDeleteDialogMetadata } from "./credential-delete-dialog";
import type { CredentialModel, useCredentialProfileEditor } from "./use-credential-profile-editor";

export type CredentialFamilyCommands = { openCreate: () => void; close: () => void };
export type CredentialFamilyProps = {
  targets: readonly InventoryTarget[];
  credentials: readonly CredentialResource[];
  busy: boolean;
  register: (_kind: string, _commands: CredentialFamilyCommands | null) => void;
  onOpen: (_kind: string) => void;
  onStateChange: (_kind: string, _state: AsyncActionState) => void;
  refresh: () => Promise<void>;
};
export type RegisteredCredentialFamily = { kind: string; Rows: ComponentType<CredentialFamilyProps> };
export type CredentialFamilyCapture = <
  State extends object,
  Row extends { connector_kind: string },
  Target extends object,
  Operation extends string,
>(
  _definition: CredentialFamilyDefinition<State, Row, Target, Operation>,
) => RegisteredCredentialFamily;
export type CredentialFamilyRegistration = {
  kind: string;
  create: (_capture: CredentialFamilyCapture) => RegisteredCredentialFamily;
};
export type CredentialFamilyEditor<
  State extends object,
  Row extends { connector_kind: string },
  Target extends object,
  Operation extends string,
> = ReturnType<typeof useCredentialProfileEditor<State, Row, Target, Operation>>;

export type CredentialFamilyDefinition<
  State extends object,
  Row extends { connector_kind: string },
  Target extends object,
  Operation extends string,
> = {
  kind: string;
  label: string;
  decodeTargets: (_targets: readonly InventoryTarget[]) => Target[];
  emptyState: (_context: { targets: Target[] }) => State;
  model: CredentialModel<State, Row, Target, Operation>;
  rows: (_context: { targets: Target[]; credentials: readonly CredentialResource[] }) => Row[];
  displayRow: (_row: Row) => CredentialDisplayRow;
  renderForm: (_context: { editor: CredentialFamilyEditor<State, Row, Target, Operation>; targets: Target[] }) => ReactNode;
  renderOperations?: (_row: Row) => ReactNode;
  deleteDialog?: (_context: { row: Row; targets: Target[] }) => CredentialDeleteDialogMetadata | null;
};
