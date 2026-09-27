import { render } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import type { ConsoleWorkspaceSlotProps } from "../../components/console/console-workspace-types";
import { gatewayTargetFixture } from "../connector-inventory-fixtures";
import { consoleWorkspaceFixture } from "../console-workspace-fixtures";
import { createSQLConsoleSlot } from "../../connectors/templates/_shared/sql-console-slot";
import { SQLConnectorConsole } from "../../connectors/templates/_shared/sql-console";
import { structuredConsoleSlotSession } from "../../connectors/templates/_shared/console-slot-session";

vi.mock("../../connectors/templates/_shared/sql-console", () => ({ SQLConnectorConsole: vi.fn(() => <div />) }));

function slotProps(overrides: Partial<ConsoleWorkspaceSlotProps> = {}): ConsoleWorkspaceSlotProps {
  return consoleWorkspaceFixture({
    target: gatewayTargetFixture({
      connector_kind: "example",
      target_name: "My database",
      config: { host: "db.test", port: 5432, database: "main" },
    }),
    session: { active: true, startedAt: "2026-09-26" },
    ...overrides,
  });
}

beforeEach(() => {
  vi.mocked(SQLConnectorConsole).mockClear();
});

it("projects only SQL-owned target fields and retains generic session and activity callbacks", () => {
  const props = slotProps();
  const Console = createSQLConsoleSlot({ label: "SQL" });
  render(<Console {...props} />);
  const native = vi.mocked(SQLConnectorConsole).mock.calls[0]?.[0];
  expect(native?.target).toEqual({ ref: props.target.ref, name: "My database", config: { host: "db.test", port: 5432, database: "main" } });
  expect(native?.session).toEqual(props.session);
  expect(native?.approvals).toBe(props.approvals);
  expect(native?.onRefreshActivity).toBe(props.onRefreshActivity);
  expect(native?.onNewStructuredSession).toBe(props.onNewStructuredSession);
});

it("does not treat a live runtime session as an active SQL session", () => {
  const props = slotProps({ session: { id: 9, status: "live" } });
  const Console = createSQLConsoleSlot({});
  render(<Console {...props} />);
  expect(vi.mocked(SQLConnectorConsole).mock.calls[0]?.[0].session).toBeNull();
});

it("rejects malformed connector-owned endpoint fields before rendering a native SQL console", () => {
  const Console = createSQLConsoleSlot({ label: "SQL" });
  for (const config of [{ host: 42 }, { database: [] }, { port: "5432" }, { port: Number.NaN }]) {
    expect(() => Console(slotProps({ target: gatewayTargetFixture({ config }) }))).toThrow("Invalid SQL console target");
  }
  expect(SQLConnectorConsole).not.toHaveBeenCalled();
});

it("uses a deterministic label when the gateway provides no target display name", () => {
  const Console = createSQLConsoleSlot({});
  render(<Console {...slotProps({ target: gatewayTargetFixture({ name: "", target_name: "" }) })} />);
  expect(vi.mocked(SQLConnectorConsole).mock.calls[0]?.[0].target.name).toBe("SQL");
});

it("decodes structured session fields without accepting malformed or live session objects", () => {
  for (const value of [
    null,
    [],
    false,
    {},
    { id: 9, status: "live" },
    { active: "true", startedAt: "today" },
    { active: true, startedAt: 42 },
  ]) {
    expect(structuredConsoleSlotSession(value)).toBeNull();
  }
  expect(structuredConsoleSlotSession({ active: false, startedAt: "", extra: "ignored" })).toEqual({ active: false, startedAt: "" });
});
