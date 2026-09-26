import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, expectTypeOf, it, vi } from "vitest";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { defineConnectorFamily, isConnectorFamilyRegistration } from "../templates/_shared/connector-family-registration";
import { captureConnectorFamily } from "./capture-connector-family";
import type {
  ConnectorFamilyCommands,
  ConnectorFamilyDefinition,
  ConnectorFamilyProps,
  RegisteredConnectorFamily,
} from "./connector-family-types";

type Form = { connector_kind: string; project_id?: string | number | null; name: string; enabled: boolean };
const profile = inventoryProfileFixture({ connector_kind: "fixture" });
const target = inventoryTargetFixture({ connector_kind: "fixture", profiles: [profile] });
type Target = typeof target & { profiles: (typeof profile)[] };
type Operation = { open: boolean; connector_kind: string; token: number };
type Definition = ConnectorFamilyDefinition<Form, typeof profile, Target, { id: number }, { id: number }, Operation>;
const save = vi.fn(async (_context: { form: Form }) => {});
const remove = vi.fn(async () => {});
const test = vi.fn(async () => ({ ok: true, data: { native: true } }));
const refresh = vi.fn(async () => {});
const onStateChange = vi.fn();
const onTestsChange = vi.fn();
let commands: ConnectorFamilyCommands | null = null;

const definition: Definition = {
  kind: "fixture",
  decodeTargets: (targets) =>
    targets.filter((entry) => entry.connector_kind === "fixture").map((entry) => ({ ...entry, profiles: entry.profiles || [] })),
  decodeCredentials: (credentials) => credentials.map(({ id }) => ({ id: Number(id) })),
  emptyForm: () => ({ connector_kind: "fixture", name: "", enabled: true }),
  emptyOperation: () => ({ open: false, connector_kind: "fixture", token: 0 }),
  model: {
    save,
    deleteTarget: remove,
    test,
    activeCredential: ({ credentials }) => credentials[0] || null,
    formFromTarget: ({ target: selected }) => ({ connector_kind: "fixture", name: selected.name, enabled: true }),
    submitLabel: () => "Save native connector",
  },
  tableModel: {
    targetEndpoint: ({ target: selected }) => selected.name,
    credentialHint: ({ profile: selected }) => selected?.label || null,
    canEdit: () => true,
    canDelete: () => true,
  },
  deleteDialog: ({ target: selected }) => ({ title: `Remove ${selected.name}` }),
  renderForm: ({ form, onChange }) => (
    <label>
      Native name
      <input value={form.name} onChange={(event) => onChange("name", event.target.value)} />
    </label>
  ),
  renderRowActions: ({ onOperation }) => (
    <button onClick={() => onOperation({ open: true, connector_kind: "fixture", token: 73 })}>Native operation</button>
  ),
  renderOperations: ({ value, onChange }) =>
    value.open ? <button onClick={() => onChange({ ...value, open: false })}>Operation {value.token}</button> : null,
};
const registration = defineConnectorFamily(definition);
const family = registration.create(captureConnectorFamily);
const register: ConnectorFamilyProps["register"] = (_kind, value) => {
  commands = value;
};
const props: Omit<ConnectorFamilyProps, "children"> = {
  targets: [target],
  credentials: [],
  projects: [
    { id: 1, name: "My Project" },
    { id: 7, name: "Other Project" },
  ],
  firstCredentialID: "",
  defaultProjectID: 1,
  connectorOptions: [{ kind: "fixture", label: "Fixture" }],
  busy: false,
  register,
  onOpen: vi.fn(),
  onSelectKind: vi.fn(),
  onStateChange,
  onTestsChange,
  refresh,
};
function Host({ busy = false }: { busy?: boolean }) {
  const Provider = family.Provider;
  const RowActions = family.tableTemplate.RowActions;
  return (
    <Provider {...props} busy={busy}>
      {RowActions ? <RowActions target={target} profile={profile} onOperation={vi.fn()} onUnderConstruction={vi.fn()} /> : null}
    </Provider>
  );
}
function renderNativeFamily(value: RegisteredConnectorFamily) {
  const Provider = value.Provider;
  const RowActions = value.tableTemplate.RowActions;
  return render(
    <Provider {...props}>
      {RowActions ? <RowActions target={target} profile={profile} onOperation={vi.fn()} onUnderConstruction={vi.fn()} /> : null}
    </Provider>,
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  commands = null;
  save.mockReset().mockResolvedValue(undefined);
  remove.mockReset().mockResolvedValue(undefined);
  test.mockReset().mockResolvedValue({ ok: true, data: { native: true } });
});

it("brands only genuine frozen registrations and retains correlated native updates", () => {
  expect(isConnectorFamilyRegistration(registration)).toBe(true);
  expect(isConnectorFamilyRegistration({ ...registration })).toBe(false);
  expect(isConnectorFamilyRegistration(null)).toBe(false);
  expect(Object.isFrozen(registration)).toBe(true);
  expectTypeOf<Parameters<Parameters<Definition["renderForm"]>[0]["onChange"]>>().toEqualTypeOf<
    ["connector_kind", string] | ["project_id", string | number | null | undefined] | ["name", string] | ["enabled", boolean]
  >();
});

it("captures a native form without remounting inputs and saves its typed state", async () => {
  const user = userEvent.setup();
  render(<Host />);
  act(() => commands?.openCreate(7));
  const input = screen.getByRole("textbox", { name: "Native name" });
  await user.type(input, "My connector");
  expect(screen.getByRole("textbox", { name: "Native name" })).toBe(input);
  expect(input).toHaveFocus();
  await user.click(screen.getByRole("button", { name: "Save native connector" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(save).toHaveBeenCalledExactlyOnceWith({
    mode: "create",
    form: { connector_kind: "fixture", project_id: 7, name: "My connector", enabled: true },
    target: null,
  });
  expect(refresh).toHaveBeenCalledOnce();
});

it("resolves inventory profiles inside the family and preserves native row operations", async () => {
  const user = userEvent.setup();
  render(<Host />);
  expect(family.tableTemplate.model.credentialHint?.({ target, profile })).toBe(profile.label);
  await user.click(screen.getByRole("button", { name: "Native operation" }));
  await user.click(screen.getByRole("button", { name: "Operation 73" }));
  await act(async () => {
    await commands?.test(target, profile);
  });
  expect(test).toHaveBeenCalledExactlyOnceWith({ target, profile });
  expect(onTestsChange).toHaveBeenLastCalledWith(
    "fixture",
    expect.objectContaining({
      [`fixture:${target.id}:${profile.id}`]: expect.objectContaining({ state: "ok", data: { native: true } }),
    }),
  );
  act(() => commands?.openEdit(target, profile));
  expect(screen.getByRole("textbox", { name: "Native name" })).toHaveValue(target.name);
});

it("rejects wrong-kind inventory before native actions and disables busy commands", async () => {
  const view = render(<Host />);
  const wrong = { ...target, connector_kind: "other" };
  expect(() => commands?.openEdit(wrong, profile)).toThrow("cannot own this target");
  expect(() => family.tableTemplate.model.canDelete?.({ target: wrong })).toThrow("cannot own this target");
  view.rerender(<Host busy />);
  act(() => commands?.openCreate());
  expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  await act(async () => {
    expect(await commands?.test(target, profile)).toBe(false);
  });
  expect(test).not.toHaveBeenCalled();
});

it("retires public callbacks when the family unmounts", () => {
  const view = render(<Host />);
  const retired = commands;
  view.unmount();
  expect(commands).toBeNull();
  act(() => retired?.openCreate());
  expect(props.onOpen).not.toHaveBeenCalled();
  expect(onTestsChange).toHaveBeenLastCalledWith("fixture", null);
});

it.each(["close", "replace", "native-close"] as const)(
  "retires pending native operation updates and completion after %s",
  async (transition) => {
    const user = userEvent.setup();
    const renderOperations = vi.fn(definition.renderOperations);
    const native = defineConnectorFamily({ ...definition, renderOperations }).create(captureConnectorFamily);
    renderNativeFamily(native);
    await user.click(screen.getByRole("button", { name: "Native operation" }));
    const retained = renderOperations.mock.calls.at(-1)?.[0];
    expect(retained?.value.token).toBe(73);
    if (transition === "close") act(() => commands?.close());
    else if (transition === "replace") await user.click(screen.getByRole("button", { name: "Native operation" }));
    else await user.click(screen.getByRole("button", { name: "Operation 73" }));
    await act(async () => {
      retained?.onChange({ open: true, connector_kind: "fixture", token: 999 });
      await retained?.onOperationComplete({ message: "Old completion" }, retained.value);
    });
    expect(screen.queryByRole("button", { name: "Operation 999" })).not.toBeInTheDocument();
    if (transition === "replace") expect(screen.getByRole("button", { name: "Operation 73" })).toBeInTheDocument();
    expect(refresh).not.toHaveBeenCalled();
    expect(onStateChange).not.toHaveBeenCalledWith("fixture", expect.objectContaining({ message: "Old completion" }));
  },
);

it.each(["close", "create"] as const)("does not reacquire recovery ownership after a pending test and %s", async (transition) => {
  let reject!: (_error: Error) => void;
  const pending = new Promise<{ ok: boolean; data: { native: boolean } }>((_resolve, fail) => {
    reject = fail;
  });
  test.mockReturnValueOnce(pending);
  const native = defineConnectorFamily({
    ...definition,
    model: { ...definition.model, operationFromError: () => ({ open: true, connector_kind: "fixture", token: 99 }) },
  }).create(captureConnectorFamily);
  renderNativeFamily(native);
  let result: Promise<boolean> | undefined;
  act(() => {
    result = commands?.test(target, profile);
  });
  act(() => {
    if (transition === "close") commands?.close();
    else commands?.openCreate();
  });
  vi.mocked(props.onOpen).mockClear();
  await act(async () => {
    reject(new Error("Late connection failure"));
    expect(await result).toBe(false);
  });
  expect(props.onOpen).not.toHaveBeenCalled();
  expect(screen.queryByRole("button", { name: "Operation 99" })).not.toBeInTheDocument();
  if (transition === "create") expect(screen.getByRole("textbox", { name: "Native name" })).toBeInTheDocument();
});
