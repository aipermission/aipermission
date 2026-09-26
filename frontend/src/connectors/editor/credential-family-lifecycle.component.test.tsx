import { StrictMode } from "react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiDelete, apiPost } from "../../lib/api";
import { inventoryProfileFixture, inventoryTargetFixture } from "../../test/connector-inventory-fixtures";
import { credentialFamilies } from "../templates/credential-registry";
import { useCredentialFamilyHost } from "./use-credential-family-host";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn(), apiPut: vi.fn(), apiDelete: vi.fn() }));
const target = inventoryTargetFixture({
  connector_kind: "redis",
  config: { host: "localhost", port: 6379 },
  profiles: [inventoryProfileFixture({ connector_kind: "redis", label: "Reader", public: { username: "reader" } })],
});
const refresh = vi.fn(async () => {});

function Host({ generation }: { generation: number | null }) {
  const host = useCredentialFamilyHost();
  const Rows = credentialFamilies.redis.Rows;
  return (
    <>
      <button onClick={() => host.openCreate("redis")}>Create test credential</button>
      <output aria-label="Host status">{host.state.state}</output>
      <table>
        <tbody>
          {generation !== null ? <Rows key={generation} targets={[target]} credentials={[]} refresh={refresh} {...host} /> : null}
        </tbody>
      </table>
    </>
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(apiPost).mockReset().mockResolvedValue({});
  vi.mocked(apiDelete).mockReset().mockResolvedValue({});
});

it.each(["save", "delete"] as const)("ignores a retired pending %s after same-kind remount", async (operation) => {
  let finish!: (_value: object) => void;
  const pending = new Promise<object>((resolve) => {
    finish = resolve;
  });
  if (operation === "save") vi.mocked(apiPost).mockReturnValueOnce(pending);
  else vi.mocked(apiDelete).mockReturnValueOnce(pending);
  const user = userEvent.setup();
  const view = render(<Host generation={1} />);
  if (operation === "save") {
    await user.click(screen.getByRole("button", { name: "Create test credential" }));
    await user.click(screen.getByRole("button", { name: "Create Redis credential" }));
  } else {
    await user.click(screen.getByRole("button", { name: "Delete credential" }));
    await user.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete credential" }));
  }
  expect(screen.getByLabelText("Host status")).toHaveTextContent(operation === "save" ? "saving" : "deleting");
  view.rerender(<Host generation={null} />);
  expect(screen.getByLabelText("Host status")).toHaveTextContent("idle");
  view.rerender(<Host generation={2} />);
  await user.click(screen.getByRole("button", { name: "Create test credential" }));
  const dialog = screen.getByRole("dialog");
  await user.type(within(dialog).getByRole("textbox", { name: "Username" }), "replacement");
  await act(async () => finish({}));
  expect(screen.getByLabelText("Host status")).toHaveTextContent("idle");
  expect(within(dialog).getByRole("textbox", { name: "Username" })).toHaveValue("replacement");
  expect(refresh).not.toHaveBeenCalled();
  await user.click(within(dialog).getByRole("button", { name: "Create Redis credential" }));
  await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  expect(refresh).toHaveBeenCalledOnce();
});

it("reports native row counts across changes, retirement, and StrictMode replay", () => {
  const onRowsChange = vi.fn();
  const Rows = credentialFamilies.redis.Rows;
  const props = {
    targets: [target],
    credentials: [],
    busy: false,
    refresh,
    register: vi.fn(),
    onOpen: vi.fn(),
    onStateChange: vi.fn(),
    onRowsChange,
  };
  const tree = (targets: typeof props.targets) => (
    <StrictMode>
      <table>
        <tbody>
          <Rows {...props} targets={targets} />
        </tbody>
      </table>
    </StrictMode>
  );
  const view = render(tree([target]));
  expect(onRowsChange.mock.calls).toEqual([
    ["redis", 1],
    ["redis", null],
    ["redis", 1],
  ]);
  onRowsChange.mockClear();
  view.rerender(tree([{ ...target, profiles: [] }]));
  expect(onRowsChange.mock.calls).toEqual([
    ["redis", null],
    ["redis", 0],
  ]);
  onRowsChange.mockClear();
  view.unmount();
  expect(onRowsChange).toHaveBeenCalledExactlyOnceWith("redis", null);
});
