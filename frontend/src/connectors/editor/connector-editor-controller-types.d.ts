export type ConnectorEditorForm = {
  connector_kind: string;
  project_id?: string | number | null;
  [field: string]: unknown;
};
export type ConnectorEditorTarget = {
  id: string | number;
  connector_kind: string;
  project_id?: string | number | null;
  profiles?: readonly unknown[];
};
export type ConnectorEditorOperation = { open?: boolean; connector_kind?: string; kind?: string };
export type ConnectorEditorFormIdentity = Pick<ConnectorEditorForm, "connector_kind" | "project_id">;

export type ConnectorEditorModel<Form, Target, Profile, Operation> = {
  syncForm?: (_context: { form: Form; firstCredentialID: string | number | null }) => Form | null;
  formFromTarget?: (_context: { target: Target; profile: Profile | null | undefined }) => Form;
  save?: (_context: { mode: "create" | "edit"; form: Form; target: Target | null }) => void | Promise<unknown>;
  operationFromError?: (_error: unknown, _context: { mode: "create" | "edit"; form: Form; target: Target | null }) => Operation | null;
  deleteTarget?: (_context: { target: Target; removeKey: boolean }) => void | Promise<unknown>;
};
export type ConnectorEditorProps<
  Form = ConnectorEditorForm,
  Target = ConnectorEditorTarget,
  Profile = Record<string, unknown>,
  Operation = ConnectorEditorOperation,
> = {
  defaultKind: string;
  firstCredentialID: string | number | null;
  defaultProjectID: string | number;
  emptyFormForKind: (_kind: string, _context?: { firstCredentialID: string | number | null }) => Form;
  modelForKind: (_kind: string) => ConnectorEditorModel<Form, Target, Profile, Operation> | null;
  onRefresh?: () => void | Promise<void>;
  onOperation?: (_operation: Operation) => boolean | void;
};
