import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useCredentialProfileEditor } from "./use-credential-profile-editor";
import * as sshModel from "../templates/ssh/model";
import * as redisModel from "../templates/redis/model";
import type { SSHCredentialState } from "../templates/ssh/form-types";
import type { SSHCredentialRow, SSHModelTarget } from "../templates/ssh/model-types";
import type { RedisProfile, RedisTarget } from "../templates/redis/form-types";

it("binds native SSH credential state to the owned credential form and update action", async () => {
  type Row = SSHCredentialRow & { connector_kind: string };
  const saveCredential = vi.fn<typeof sshModel.saveCredential>().mockResolvedValue({ message: "Credential updated." });
  const nativeModel = { ...sshModel, saveCredential };
  const row: Row = { id: 9, connector_kind: "ssh", name: "My key", kind: "rsa" };
  const { result } = renderHook(() =>
    useCredentialProfileEditor<SSHCredentialState, Row, SSHModelTarget, "create" | "import" | "update">({
      defaultKind: "ssh",
      targets: [],
      emptyStateForKind: () => sshModel.emptyCredentialState(),
      modelForKind: () => nativeModel,
    }),
  );
  act(() => {
    result.current.openEdit(row);
  });
  const props = sshModel.credentialFormProps({
    formState: result.current.formState,
    setFormState: result.current.setFormState,
    formMode: result.current.drawer.mode,
    state: result.current.actionState,
    onSubmit: result.current.save,
  });
  expect(props.form.key_type).toBe("rsa");
  act(() => {
    props.onFormChange({ ...props.form, name: "Renamed key" });
  });
  await act(async () => {
    expect(await result.current.save(null, "update")).toBe(true);
  });
  expect(saveCredential).toHaveBeenCalledWith(
    expect.objectContaining({
      row,
      operation: "update",
      formState: expect.objectContaining({ form: { name: "Renamed key", key_type: "rsa" } }),
    }),
  );
  expect(result.current.drawer.open).toBe(false);
  expect(result.current.formState.importForm.private_key).toBe("");
});

it("binds native Redis profile state and passes a mutable snapshot of readonly targets", async () => {
  type State = ReturnType<typeof redisModel.emptyCredentialState>;
  type Row = { connector_kind: string; id: number; target_id: number; name: string; profile: RedisProfile; target: RedisTarget };
  const saveCredential = vi.fn<typeof redisModel.saveCredential>().mockResolvedValue({ message: "Cache profile updated." });
  const target: RedisTarget = { id: 3, connector_kind: "redis", name: "Cache" };
  const targets = Object.freeze([target]);
  const row: Row = {
    id: 5,
    target_id: 3,
    connector_kind: "redis",
    name: "Reader",
    profile: { id: 5, kind: "username_password", label: "Reader", public: { username: "reader" } },
    target,
  };
  const { result } = renderHook(() =>
    useCredentialProfileEditor<State, Row, RedisTarget, "create" | "update">({
      defaultKind: "redis",
      targets,
      emptyStateForKind: (_kind, context) => redisModel.emptyCredentialState(context),
      modelForKind: () => ({ ...redisModel, saveCredential }),
    }),
  );
  act(() => {
    result.current.openEdit(row);
  });
  expect(result.current.formState.form.username).toBe("reader");
  act(() => {
    result.current.setFormState((current) => ({ form: { ...current.form, password: "temporary-test-value" } }));
  });
  await act(async () => {
    await result.current.save(null, "update");
  });
  const saved = saveCredential.mock.calls[0]?.[0];
  expect(saved?.targets).toEqual(targets);
  expect(saved?.targets).not.toBe(targets);
  expect(saved?.formState.form.password).toBe("temporary-test-value");
  expect(result.current.formState.form.password).toBe("");
});
