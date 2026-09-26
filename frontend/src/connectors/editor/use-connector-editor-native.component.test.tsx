import { act, renderHook } from "@testing-library/react";
import { expect, it, vi } from "vitest";
import { useConnectorEditor } from "./use-connector-editor";
import * as sshModel from "../templates/ssh/model";
import * as redisModel from "../templates/redis/model";
import type { SSHForm, SSHProfile } from "../templates/ssh/form-types";
import type { SSHModelTarget } from "../templates/ssh/model-types";
import type { SSHOperation } from "../templates/ssh/operation-types";
import type { RedisModelForm, RedisProfile, RedisTarget } from "../templates/redis/form-types";

it("keeps native SSH form, selected profile, and save context correlated", async () => {
  const save = vi.fn<typeof sshModel.save>().mockResolvedValue(undefined);
  const nativeModel = { ...sshModel, save };
  const profile: SSHProfile = { id: 11, label: "Admin", public: { username: "root", ssh_key_id: 9 } };
  const target: SSHModelTarget = {
    id: "target-3",
    connector_kind: "ssh",
    name: "My host",
    profiles: [profile],
    config: { host: "host.test", port: 22 },
  };
  const { result } = renderHook(() =>
    useConnectorEditor<SSHForm, SSHModelTarget, SSHProfile, SSHOperation>({
      defaultKind: "ssh",
      defaultProjectID: 7,
      firstCredentialID: 9,
      emptyFormForKind: (_kind, context) => sshModel.emptyForm(context),
      modelForKind: () => nativeModel,
    }),
  );

  act(() => {
    expect(result.current.openEdit(target, profile)).toBe(true);
  });
  expect(result.current.form.username).toBe("root");
  expect(result.current.form.ssh_key_id).toBe("9");
  act(() => {
    result.current.updateField("setup_later", true);
    result.current.updateField("project_id", null);
  });
  await act(async () => {
    expect(await result.current.save()).toBe(true);
  });
  expect(save).toHaveBeenCalledWith({
    mode: "edit",
    form: expect.objectContaining({ host: "host.test", setup_later: true, project_id: null }),
    target,
  });
  expect(result.current.drawer.open).toBe(false);
});

it("keeps Redis product and transport fields in the native controller", async () => {
  type Target = RedisTarget & { id: number; connector_kind: string };
  const save = vi.fn<typeof redisModel.save>().mockResolvedValue(undefined);
  const nativeModel = { ...redisModel, save };
  const profile: RedisProfile = { id: 5, kind: "username_password", label: "Cache", public: { username: "reader" } };
  const target: Target = {
    id: 3,
    connector_kind: "redis",
    name: "Cache",
    config: { server_family: "valkey", host: "cache.test", connection_mode: "direct" },
    profiles: [profile],
  };
  const { result } = renderHook(() =>
    useConnectorEditor<RedisModelForm, Target, RedisProfile>({
      defaultKind: "redis",
      defaultProjectID: 7,
      firstCredentialID: null,
      emptyFormForKind: () => redisModel.emptyForm(),
      modelForKind: () => nativeModel,
    }),
  );
  act(() => {
    result.current.openEdit(target, profile);
  });
  expect(result.current.form.server_family).toBe("valkey");
  expect(result.current.form.username).toBe("reader");
  act(() => {
    result.current.updateField("database", 2);
    result.current.updateField("password", "temporary-test-value");
  });
  await act(async () => {
    await result.current.save();
  });
  expect(save).toHaveBeenCalledWith({
    mode: "edit",
    target,
    form: expect.objectContaining({ server_family: "valkey", database: 2, password: "temporary-test-value" }),
  });
  expect(result.current.form.password).toBe("");
});
