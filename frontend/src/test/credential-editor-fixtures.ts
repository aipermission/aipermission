import { renderHook } from "@testing-library/react";
import { vi } from "vitest";
import type { useCredentialProfileEditor } from "../connectors/editor/use-credential-profile-editor";

export type FormState = { form: { connector_kind?: string; label: string; password: string } };
export type Row = { id: number; connector_kind: string };
type Model = NonNullable<ReturnType<Parameters<typeof useCredentialProfileEditor<FormState, Row>>[0]["modelForKind"]>>;
export const emptyState = (kind: string) => ({ form: { connector_kind: kind, label: "", password: "" } });

export function credentialEditorRenderer(useEditor: typeof useCredentialProfileEditor) {
  return function renderEditor(model: Model | null, onRefresh = vi.fn<() => Promise<void>>(async () => {})) {
    const hook = renderHook(() =>
      useEditor<FormState, Row>({
        defaultKind: "example",
        targets: [{ id: 4, connector_kind: "example" }],
        emptyStateForKind: emptyState,
        modelForKind: () => model,
        onRefresh,
      }),
    );
    return { ...hook, onRefresh };
  };
}

export function deferred() {
  let resolve!: () => void;
  let reject!: (_error: Error) => void;
  const promise = new Promise<void>((done, fail) => {
    resolve = done;
    reject = fail;
  });
  return { promise, resolve, reject };
}
