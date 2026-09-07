import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiGet } from "../../lib/api";
import { useConnectorInventory } from "./use-connector-inventory";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn() }));

function InventoryHarness({ loadUnifiedTargets = vi.fn() }) {
  const inventory = useConnectorInventory({ loadUnifiedTargets });
  const target = inventory.targets.data[0];
  return (
    <div>
      <p data-testid="states">{`${inventory.catalog.state}:${inventory.targets.state}:${inventory.projects.state}`}</p>
      <p data-testid="kinds">{inventory.availableConnectorKinds.join(",")}</p>
      <p data-testid="project">{inventory.defaultProjectID}</p>
      <p data-testid="profile">{target ? inventory.profileSelections[`${target.connector_kind}:${target.id}`] : ""}</p>
      <p data-testid="warnings">{inventory.warnings.join("|")}</p>
      <button type="button" onClick={() => inventory.selectProfile(target, 22)} disabled={!target}>
        Select profile
      </button>
      <button type="button" onClick={() => void inventory.refresh()}>
        Refresh
      </button>
    </div>
  );
}

beforeEach(() => {
  apiGet.mockReset();
  apiGet.mockImplementation((path) => {
    if (path === "/api/connectors") return Promise.resolve({ items: [{ kind: "ssh" }, { kind: "backend-only" }] });
    if (path === "/api/connectors/ssh") return Promise.resolve({ kind: "ssh", label: "SSH" });
    if (path === "/api/connectors/backend-only") return Promise.reject(new Error("detail unavailable"));
    if (path === "/api/projects") return Promise.resolve({ items: [{ id: 7, slug: "ungrouped" }] });
    if (path === "/api/connector-targets/inventory") {
      return Promise.resolve({ items: [{ id: 3, connector_kind: "ssh", profiles: [{ id: 11 }, { id: 22 }] }] });
    }
    throw new Error(`Unexpected API path: ${path}`);
  });
});

it("owns connector catalog, inventory, projects, and deterministic profile selection", async () => {
  const user = userEvent.setup();
  const loadUnifiedTargets = vi.fn().mockResolvedValue(undefined);
  render(<InventoryHarness loadUnifiedTargets={loadUnifiedTargets} />);

  expect(await screen.findByTestId("states")).toHaveTextContent("ready:ready:ready");
  expect(screen.getByTestId("kinds")).toHaveTextContent("ssh");
  expect(screen.getByTestId("project")).toHaveTextContent("7");
  await waitFor(() => expect(screen.getByTestId("profile")).toHaveTextContent("11"));
  expect(screen.getByTestId("warnings")).toHaveTextContent("backend-only");
  expect(screen.getByTestId("warnings")).toHaveTextContent("detail unavailable");
  await user.click(screen.getByRole("button", { name: "Select profile" }));
  expect(screen.getByTestId("profile")).toHaveTextContent("22");
  expect(loadUnifiedTargets).toHaveBeenCalledOnce();
});

it("aborts superseded inventory requests", async () => {
  const signals = [];
  apiGet.mockImplementation((path, options) => {
    if (path === "/api/connectors") return Promise.resolve({ items: [] });
    if (path === "/api/projects") return Promise.resolve({ items: [] });
    if (path === "/api/connector-targets/inventory") {
      signals.push(options.signal);
      return new Promise(() => {});
    }
    throw new Error(`Unexpected API path: ${path}`);
  });
  const user = userEvent.setup();
  render(<InventoryHarness loadUnifiedTargets={vi.fn().mockResolvedValue(undefined)} />);
  await waitFor(() => expect(signals).toHaveLength(1));
  await user.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(signals).toHaveLength(2));
  expect(signals[0].aborted).toBe(true);
  expect(signals[1].aborted).toBe(false);
});
