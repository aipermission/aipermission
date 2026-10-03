import { renderHook } from "@testing-library/react";
import { vi } from "vitest";
import { useConnectorEditor } from "../connectors/editor/use-connector-editor";
import type { ConnectorEditorProps } from "../connectors/editor/connector-editor-controller-types";

type EditorProps = ConnectorEditorProps;
export type EditorModel = NonNullable<ReturnType<EditorProps["modelForKind"]>>;
export type SyncContext = Parameters<NonNullable<EditorModel["syncForm"]>>[0];

export function renderEditor(model: EditorModel | null) {
  const onRefresh = vi.fn(async () => {});
  const onOperation = vi.fn(() => true);
  const modelForKind = vi.fn(() => model);
  const hook = renderHook(
    ({ firstCredentialID }) =>
      useConnectorEditor({
        defaultKind: "example",
        firstCredentialID,
        defaultProjectID: "2",
        emptyFormForKind: (kind) => ({ connector_kind: kind, name: "", project_id: "" }),
        modelForKind,
        onRefresh,
        onOperation,
      }),
    { initialProps: { firstCredentialID: "4" } },
  );
  return { ...hook, onRefresh, onOperation, modelForKind };
}

export function deferredEditorMutation() {
  let resolve!: () => void;
  let reject!: (_reason: unknown) => void;
  const promise = new Promise<void>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
