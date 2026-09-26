import type { ComponentType, Dispatch, ReactNode, SetStateAction } from "react";
import type { InventoryProfile, InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { CredentialResource, GatewayTarget } from "../../lib/gateway-contracts/core-resource-contracts";
import type { AsyncActionState } from "../../lib/use-async-action";
import type { useConnectorEditor } from "./use-connector-editor";
import type {
  ConnectorEditorFormIdentity,
  ConnectorEditorModel,
  ConnectorEditorOperation,
  ConnectorEditorTarget,
} from "./connector-editor-controller-types";
import type { ConnectorDeleteDialog } from "./connector-editor-dialog-types";
import type { TargetTableTemplate } from "./connector-target-table-types";
import type { ConnectorConnectionResult, ConnectorTestState } from "./use-connector-connection-tests";
import type { ConnectorFieldChange } from "../templates/_shared/connector-form-types";

export type ConnectorFamilyCommands = {
  openCreate: (_projectID?: string | number) => void;
  openEdit: (_target: InventoryTarget, _profile: InventoryProfile | null) => void;
  requestDelete: (_target: InventoryTarget) => void;
  test: (_target: InventoryTarget, _profile: InventoryProfile | null) => Promise<boolean>;
  close: () => void;
};
export type ConnectorFamilyProps = {
  children: ReactNode;
  targets: readonly InventoryTarget[];
  credentials: readonly CredentialResource[];
  projects: { id: number | string; name: string }[];
  firstCredentialID: string;
  defaultProjectID: number | string;
  connectorOptions: { kind: string; label: string }[];
  busy: boolean;
  register: (_kind: string, _commands: ConnectorFamilyCommands | null) => void;
  onOpen: (_kind: string) => void;
  onSelectKind: (_kind: string, _projectID: string | number | null | undefined) => void;
  onStateChange: (_kind: string, _state: AsyncActionState) => void;
  onTestsChange: (_kind: string, _tests: Record<string, ConnectorTestState> | null) => void;
  refresh: () => Promise<void>;
};
export type RegisteredConnectorFamily = {
  kind: string;
  Provider: ComponentType<ConnectorFamilyProps>;
  tableTemplate: TargetTableTemplate;
};
export type ConnectorOperationCompletion = {
  message?: string;
  testKey?: string;
  test?: ConnectorConnectionResult;
  startConsoleSession?: boolean;
};
export type ConnectorFamilyTarget<Profile> = Omit<ConnectorEditorTarget, "profiles"> & { id: number; name?: string; profiles?: Profile[] };
export type ConnectorFamilyProfile = { id: number };
export type ConnectorFamilyEditor<
  Form extends ConnectorEditorFormIdentity,
  Target extends ConnectorEditorTarget,
  Profile extends object,
  Operation extends ConnectorEditorOperation,
> = ReturnType<typeof useConnectorEditor<Form, Target, Profile, Operation>>;
type FormContext<Form, Credential> = { form: Form; credentials: Credential[]; mode: "create" | "edit"; state: AsyncActionState };
type TargetContext<Target, Profile, Credential> = {
  target: Target;
  profile: Profile | null;
  credentials: Credential[];
  runtime?: GatewayTarget;
};

export type ConnectorFamilyDefinition<
  Form extends ConnectorEditorFormIdentity,
  Profile extends ConnectorFamilyProfile,
  Target extends ConnectorFamilyTarget<Profile>,
  Credential extends object,
  Active extends object,
  Operation extends ConnectorEditorOperation & { open: boolean },
> = {
  kind: string;
  decodeTargets: (_targets: readonly InventoryTarget[]) => Target[];
  decodeCredentials: (_credentials: readonly CredentialResource[]) => Credential[];
  firstCredentialID?: (_credentials: Credential[]) => string | number | null;
  emptyForm: (_context?: { firstCredentialID?: number | string | null }) => Form;
  emptyOperation: () => Operation;
  model: Omit<ConnectorEditorModel<Form, Target, Profile, Operation>, "operationFromError"> & {
    operationFromError?: (
      _error: unknown,
      _context:
        | { mode: "create" | "edit"; form: Form; target: Target | null }
        | { operation: "test"; target: Target; profile: Profile; testKey: string },
    ) => Operation | null;
    test: (_context: { target: Target; profile: Profile }) => Promise<ConnectorConnectionResult>;
    activeCredential: (_context: { credentials: Credential[]; form: Form }) => Active | null;
    submitLabel: (_context: Omit<FormContext<Form, Credential>, "credentials">) => string;
    submitDisabled?: (_context: FormContext<Form, Credential>) => boolean;
  };
  tableModel: {
    targetEndpoint: (_context: TargetContext<Target, Profile, Credential>) => string;
    credentialHint: (_context: TargetContext<Target, Profile, Credential>) => string | null;
    canEdit: (_context: TargetContext<Target, Profile, Credential>) => boolean;
    canDelete: (_context: { target: Target }) => boolean;
  };
  deleteDialog: (_context: { target: Target }) => ConnectorDeleteDialog;
  renderForm: (_context: {
    form: Form;
    mode: "create" | "edit";
    credentials: Credential[];
    activeCredential: Active | null;
    targets: InventoryTarget[];
    onChange: ConnectorFieldChange<Form>;
  }) => ReactNode;
  renderRowActions: (_context: {
    target: Target;
    profile: Profile | null;
    onOperation: (_operation: Operation) => void;
    onUnderConstruction: (_label: string) => void;
  }) => ReactNode;
  renderOperations: (_context: {
    value: Operation;
    credentials: Credential[];
    onChange: Dispatch<SetStateAction<Operation>>;
    onOperationComplete: (_result: ConnectorOperationCompletion, _operation: ConnectorEditorOperation) => Promise<void>;
  }) => ReactNode;
};
export type ConnectorFamilyCapture = <
  Form extends ConnectorEditorFormIdentity,
  Profile extends ConnectorFamilyProfile,
  Target extends ConnectorFamilyTarget<Profile>,
  Credential extends object,
  Active extends object,
  Operation extends ConnectorEditorOperation & { open: boolean },
>(
  _definition: ConnectorFamilyDefinition<Form, Profile, Target, Credential, Active, Operation>,
) => RegisteredConnectorFamily;
export type ConnectorFamilyRegistration = { kind: string; create: (_capture: ConnectorFamilyCapture) => RegisteredConnectorFamily };
