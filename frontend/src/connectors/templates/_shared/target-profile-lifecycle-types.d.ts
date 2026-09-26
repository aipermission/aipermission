import type { Dispatch, FormEvent, SetStateAction } from "react";

export interface LifecycleProfile {
  id?: string | number;
  label?: string;
  kind?: string;
  risk_label?: string;
}

export interface UsernamePasswordProfile extends LifecycleProfile {
  id: number;
  label: string;
  kind: string;
  public?: { username?: string };
}

export interface LifecycleTarget<Profile extends LifecycleProfile = LifecycleProfile> {
  id?: string | number;
  name?: string;
  connector_kind?: string;
  profiles?: Profile[];
}

export interface LifecycleTargetForm {
  project_id?: string | number | null;
  profile_id?: string | number;
}

export interface LifecycleCredentialForm {
  target_id?: string | number;
}

export interface LifecycleCredentialRow<Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>> {
  id: string | number;
  target_id: string | number;
  profile?: Profile;
  target?: Target;
}

export interface LifecycleSaveContext<Form, Target> {
  mode: string;
  form: Form;
  target?: Target | null;
}

export interface LifecycleCredentialContext<Form, Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>> {
  operation: string;
  form: Form;
  row?: LifecycleCredentialRow<Profile, Target> | null;
  targets: Target[];
}

export type LifecycleProfileOperation = "target-create" | "target-update" | "credential-create" | "credential-update";

export interface LifecycleMessageContext<Form, Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>> {
  form: Form;
  row: LifecycleCredentialRow<Profile, Target> | null;
  target: Target | null;
}

export type LifecycleMessage<Form, Profile extends LifecycleProfile, Target extends LifecycleTarget<Profile>> =
  string | ((_context: LifecycleMessageContext<Form, Profile, Target>) => string);

export interface LifecycleOptions<
  Form extends LifecycleTargetForm,
  CredentialForm extends LifecycleCredentialForm,
  Profile extends LifecycleProfile,
  Target extends LifecycleTarget<Profile>,
> {
  connectorKind: string;
  connectorLabel: string;
  targetPayload: (_form: Form) => Record<string, unknown>;
  profilePayload: (
    _form: Form | CredentialForm,
    _context: { operation: LifecycleProfileOperation; profile: Profile | null },
  ) => Record<string, unknown>;
  credentialCreatedMessage?: LifecycleMessage<CredentialForm, Profile, Target>;
  credentialUpdatedMessage?: LifecycleMessage<CredentialForm, Profile, Target>;
  credentialMissingMessage?: string;
  unsupportedCredentialMessage?: string;
  beforeSave?: ((_context: LifecycleSaveContext<Form, Target>) => unknown) | null;
  beforeSaveCredential?: ((_context: LifecycleCredentialContext<CredentialForm, Profile, Target>) => unknown) | null;
}

export interface CredentialFormPropsContext<Form, FormState extends { form: Form }, Status, Target> {
  targets: Target[];
  formState: FormState;
  setFormState: Dispatch<SetStateAction<FormState>>;
  formMode: "create" | "edit";
  state: Status;
  onSubmit: (_event: FormEvent<HTMLFormElement>, _operation: "update" | "create") => unknown;
}

export interface LifecycleCredentialFormProps<Form, Status, Target> {
  form: Form;
  formMode: "create" | "edit";
  targets: Target[];
  state: Status;
  onChange: Dispatch<SetStateAction<Form>>;
  onSubmit: (_event: FormEvent<HTMLFormElement>) => unknown;
}
