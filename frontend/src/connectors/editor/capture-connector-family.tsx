import {
  createContext,
  Fragment,
  useCallback,
  useContext,
  useEffect,
  useEffectEvent,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { ConnectorEditorDrawer, DeleteConnectorDialog } from "./connector-page-dialogs";
import { useConnectorEditor } from "./use-connector-editor";
import { useConnectorConnectionTests } from "./use-connector-connection-tests";
import type { ConnectorEditorFormIdentity, ConnectorEditorOperation } from "./connector-editor-controller-types";
import type { ConnectorFormSlotProps } from "./connector-editor-dialog-types";
import type { TargetRowActionsProps, TargetTableTemplate } from "./connector-target-table-types";
import type { InventoryProfile, InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type {
  ConnectorFamilyDefinition,
  ConnectorFamilyProfile,
  ConnectorFamilyProps,
  ConnectorFamilyTarget,
  ConnectorOperationCompletion,
  RegisteredConnectorFamily,
} from "./connector-family-types";

// A family closure retains native payload types; the host receives only inventory commands.
export function captureConnectorFamily<
  Form extends ConnectorEditorFormIdentity,
  Profile extends ConnectorFamilyProfile,
  Target extends ConnectorFamilyTarget<Profile>,
  Credential extends object,
  Active extends object,
  Operation extends ConnectorEditorOperation & { open: boolean },
>(definition: ConnectorFamilyDefinition<Form, Profile, Target, Credential, Active, Operation>): RegisteredConnectorFamily {
  const OperationContext = createContext<((_operation: Operation) => void) | null>(null);

  function decodeTarget(target: InventoryTarget) {
    const decoded = definition.decodeTargets([target]).find((entry) => entry.id === target.id);
    if (!decoded) throw new Error(`Connector family ${definition.kind} cannot own this target.`);
    return decoded;
  }
  function selectedProfile(target: Target, profile: InventoryProfile | null) {
    return profile ? target.profiles?.find((entry) => entry.id === profile.id) || null : null;
  }
  const tableTemplate: TargetTableTemplate = {
    model: {
      targetEndpoint: ({ target, profile, credentials = [], runtime }) => {
        const native = decodeTarget(target);
        return definition.tableModel.targetEndpoint({
          target: native,
          profile: selectedProfile(native, profile),
          credentials: definition.decodeCredentials(credentials),
          runtime,
        });
      },
      credentialHint: ({ target, profile, credentials = [], runtime }) => {
        const native = decodeTarget(target);
        return definition.tableModel.credentialHint({
          target: native,
          profile: selectedProfile(native, profile),
          credentials: definition.decodeCredentials(credentials),
          runtime,
        });
      },
      canEdit: ({ target, profile }) => {
        const native = decodeTarget(target);
        return definition.tableModel.canEdit({ target: native, profile: selectedProfile(native, profile), credentials: [] });
      },
      canDelete: ({ target }) => definition.tableModel.canDelete({ target: decodeTarget(target) }),
    },
    RowActions,
  };
  function RowActions({ target, profile, onUnderConstruction }: TargetRowActionsProps) {
    const onOperation = useContext(OperationContext);
    const native = decodeTarget(target);
    if (!onOperation) return null;
    return definition.renderRowActions({ target: native, profile: selectedProfile(native, profile), onOperation, onUnderConstruction });
  }
  function FormSlot(props: ConnectorFormSlotProps<Form, Credential, InventoryTarget, Active>) {
    return definition.renderForm(props);
  }
  function Provider(props: ConnectorFamilyProps) {
    const {
      children,
      targets,
      projects,
      firstCredentialID,
      defaultProjectID,
      connectorOptions,
      busy,
      register,
      onOpen,
      onSelectKind,
      refresh,
    } = props;
    const credentials = useMemo(() => definition.decodeCredentials(props.credentials), [props.credentials]);
    const [operation, setOperation] = useState(definition.emptyOperation);
    const operationValue = useRef(operation);
    const operationOwner = useRef(0);
    const [operationGeneration, setOperationGeneration] = useState(0);
    const replaceOperation = useCallback((value: Operation) => {
      operationOwner.current += 1;
      setOperationGeneration(operationOwner.current);
      operationValue.current = value;
      setOperation(value);
    }, []);
    const current = useRef({ mounted: false, busy, onOpen });
    useLayoutEffect(() => {
      current.current = { mounted: true, busy, onOpen };
      return () => {
        current.current.mounted = false;
      };
    }, [busy, onOpen]);
    const recover = useCallback(
      (value: Operation) => {
        if (!current.current.mounted || !value.open) return false;
        current.current.onOpen(definition.kind);
        if (!current.current.mounted) return false;
        replaceOperation(value);
        return true;
      },
      [replaceOperation],
    );
    const openOperation = useCallback(
      (value: Operation) => {
        if (!current.current.busy) recover(value);
      },
      [recover],
    );
    const editor = useConnectorEditor<Form, Target, Profile, Operation>({
      defaultKind: definition.kind,
      firstCredentialID: definition.firstCredentialID ? definition.firstCredentialID(credentials) : firstCredentialID,
      defaultProjectID,
      emptyFormForKind: (_kind, context) => definition.emptyForm(context),
      modelForKind: () => definition.model,
      onRefresh: refresh,
      onOperation: recover,
    });
    const tests = useConnectorConnectionTests<Target, Profile, Operation>({
      modelForKind: () => definition.model,
      onOperation: recover,
      claimRecovery: () => {
        const owner = operationOwner.current;
        return () => current.current.mounted && operationOwner.current === owner;
      },
    });
    const handlers = useRef({ editor, tests });
    useLayoutEffect(() => {
      handlers.current = { editor, tests };
    }, [editor, tests]);
    const close = useCallback(() => {
      if (!current.current.mounted) return;
      handlers.current.editor.closeEditor();
      handlers.current.editor.closeDelete();
      replaceOperation(definition.emptyOperation());
    }, [replaceOperation]);
    const activate = useCallback(() => {
      if (!current.current.mounted || current.current.busy) return false;
      replaceOperation(definition.emptyOperation());
      current.current.onOpen(definition.kind);
      return current.current.mounted && !current.current.busy;
    }, [replaceOperation]);
    useEffect(() => {
      register(definition.kind, {
        close,
        openCreate: (projectID) => {
          if (!activate()) return;
          handlers.current.editor.openCreate(definition.kind, projectID);
        },
        openEdit: (target, profile) => {
          if (!activate()) return;
          const native = decodeTarget(target);
          handlers.current.editor.openEdit(native, selectedProfile(native, profile));
        },
        requestDelete: (target) => {
          if (!activate()) return;
          handlers.current.editor.requestDelete(decodeTarget(target));
        },
        test: (target, profile) => {
          if (!current.current.mounted || current.current.busy) return Promise.resolve(false);
          const native = decodeTarget(target);
          return handlers.current.tests.run(native, selectedProfile(native, profile));
        },
      });
      return () => register(definition.kind, null);
    }, [register, activate, close]);
    const reportState = useEffectEvent(() => props.onStateChange(definition.kind, editor.actionState));
    useEffect(() => {
      reportState();
    }, [editor.actionState]);
    const reportTests = useEffectEvent((value: typeof tests.tests | null) => props.onTestsChange(definition.kind, value));
    useEffect(() => {
      reportTests(tests.tests);
      return () => reportTests(null);
    }, [tests.tests]);
    async function completeOperation(result: ConnectorOperationCompletion, identity: ConnectorEditorOperation) {
      if (!current.current.mounted || operationOwner.current !== operationGeneration) return;
      if (result.testKey && result.test) {
        tests.applyOperationResult(result.testKey, result.test);
        return;
      }
      editor.completeOperation(result, identity);
      await refresh();
    }
    const safeEditor = {
      ...editor,
      selectKind: (kind: string) => {
        if (!current.current.mounted || current.current.busy) return;
        onSelectKind(kind, editor.form.project_id);
      },
      save: async (...args: Parameters<typeof editor.save>) => {
        if (!current.current.mounted || current.current.busy) return false;
        return editor.save(...args);
      },
    };
    const submission = {
      submitDisabled: () =>
        busy ||
        Boolean(definition.model.submitDisabled?.({ state: editor.actionState, mode: editor.drawer.mode, form: editor.form, credentials })),
      submitLabel: () => definition.model.submitLabel({ state: editor.actionState, mode: editor.drawer.mode, form: editor.form }),
    };
    return (
      <OperationContext value={openOperation}>
        {children}
        <ConnectorEditorDrawer<Form, Credential, InventoryTarget, Active, Target>
          drawer={editor.drawer}
          form={editor.form}
          state={editor.actionState}
          connectorOptions={connectorOptions}
          projects={projects}
          credentials={credentials}
          targets={targets.slice()}
          activeConnectorModel={submission}
          activeCredential={definition.model.activeCredential({ credentials, form: editor.form })}
          FormTemplate={FormSlot}
          onProjectChange={(projectID) => editor.updateField("project_id", projectID)}
          editor={safeEditor}
        />
        <DeleteConnectorDialog
          value={editor.deleteDialog}
          dialog={editor.deleteDialog.target ? definition.deleteDialog({ target: editor.deleteDialog.target }) : null}
          state={editor.actionState}
          onDelete={(removeKey) => !current.current.busy && editor.remove(removeKey)}
          onClose={editor.closeDelete}
        />
        <Fragment key={operationGeneration}>
          {definition.renderOperations({
            value: operation,
            credentials,
            onChange: (value) => {
              if (!current.current.mounted || operationOwner.current !== operationGeneration) return;
              const next = typeof value === "function" ? value(operationValue.current) : value;
              if (!next.open) replaceOperation(next);
              else {
                operationValue.current = next;
                setOperation(next);
              }
            },
            onOperationComplete: completeOperation,
          })}
        </Fragment>
      </OperationContext>
    );
  }
  return Object.freeze({ kind: definition.kind, Provider, tableTemplate });
}
