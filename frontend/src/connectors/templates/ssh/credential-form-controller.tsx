import { useState } from "react";
import { useRequestGuard } from "../../../lib/request-guard";
import { SSHCredentialFormTemplate } from "./credential-form";
import { credentialFormProps, emptyCredentialState } from "./model";
import { keyNameFromFilename } from "./model-helpers";
import type { SSHCredentialPropsContext, SSHCredentialFormProps } from "./model-types";

export function SSHCredentialFormController(context: SSHCredentialPropsContext) {
  return <SSHCredentialFormTemplate {...useCredentialFormProps(context)} />;
}

function useCredentialFormProps({ formState, setFormState, formMode, state, onSubmit }: SSHCredentialPropsContext): SSHCredentialFormProps {
  const guard = useRequestGuard(`${formMode}:${formState.mode}`);
  const [fileState, setFileState] = useState({ pending: false, error: "" });

  function retireFile() {
    guard.invalidate("import-file");
    setFileState({ pending: false, error: "" });
  }

  const props = credentialFormProps({
    formState,
    setFormState,
    formMode,
    onSubmit,
    state: fileState.pending ? { state: "reading" } : fileState.error ? { state: "error", error: fileState.error } : state,
    onReadImportFile: async (event) => {
      const file = event.currentTarget.files?.[0];
      event.currentTarget.value = "";
      if (!file) return;
      const request = guard.begin("import-file");
      setFileState({ pending: true, error: "" });
      try {
        const text = await file.text();
        if (!request.isCurrent()) return;
        const defaultName = emptyCredentialState().importForm.name;
        setFormState((current) => ({
          ...current,
          importForm: {
            ...current.importForm,
            name: current.importForm.name === defaultName ? keyNameFromFilename(file.name, defaultName) : current.importForm.name,
            private_key: text,
          },
        }));
        setFileState({ pending: false, error: "" });
      } catch {
        if (request.isCurrent())
          setFileState({ pending: false, error: "The key file could not be read. Choose it again or paste the key." });
      } finally {
        request.complete();
      }
    },
  });
  return {
    ...props,
    onModeChange: (mode) => {
      if (mode !== formState.mode) retireFile();
      props.onModeChange(mode);
    },
    onImportFormChange: (importForm) => {
      if (importForm.private_key !== formState.importForm.private_key) retireFile();
      props.onImportFormChange(importForm);
    },
    onImport: (event) => {
      if (fileState.pending) {
        event.preventDefault();
        return;
      }
      setFileState({ pending: false, error: "" });
      return props.onImport(event);
    },
  };
}
