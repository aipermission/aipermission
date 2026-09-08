import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { VaultRow } from "./vault-row";

const projects = [{ id: 2, name: "Shared project" }];
const baseItem = {
  id: 1,
  name: "API_TOKEN",
  provider: "Example",
  environment: "production",
  secret_type: "api_token",
  owner_project_name: "My project",
  project_ids: [2],
  tags: ["api", "deploy", "production", "rotated"],
  last_used_at: "2026-01-01T00:00:00Z",
  expiry_warning_days: 14,
};

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date("2026-01-10T00:00:00Z"));
});

afterEach(() => {
  vi.useRealTimers();
});

it("renders Vault metadata and dispatches every row action", () => {
  const actions = {
    onBindings: vi.fn(),
    onReveal: vi.fn(),
    onReplace: vi.fn(),
    onEdit: vi.fn(),
    onDelete: vi.fn(),
  };
  renderRow({ ...baseItem, expires_at: "2026-01-15T00:00:00Z" }, actions);

  expect(screen.getByText("API_TOKEN")).toBeInTheDocument();
  expect(screen.getByText("My project")).toBeInTheDocument();
  expect(screen.getByText("Shared project")).toBeInTheDocument();
  expect(screen.getByText("+1")).toBeInTheDocument();
  expect(screen.getByText("5d left")).toBeInTheDocument();
  for (const [title, callback] of [
    ["Default session bindings", actions.onBindings],
    ["Reveal and copy", actions.onReveal],
    ["Replace local value", actions.onReplace],
    ["Edit metadata", actions.onEdit],
    ["Delete", actions.onDelete],
  ]) {
    fireEvent.click(screen.getByTitle(title));
    expect(callback).toHaveBeenCalledOnce();
  }
});

it.each([
  [undefined, "Never"],
  ["2026-01-09T00:00:00Z", "Expired"],
  ["2026-02-10T00:00:00Z", new Date("2026-02-10T00:00:00Z").toLocaleDateString()],
])("renders expiry %s as %s", (expiresAt, label) => {
  renderRow({ ...baseItem, expires_at: expiresAt, last_used_at: "" });
  expect(screen.getByText(label, { selector: "span" })).toBeInTheDocument();
  expect(screen.getByText("Never", { selector: "td" })).toBeInTheDocument();
});

function renderRow(item, actions = {}) {
  return render(
    <table>
      <tbody>
        <VaultRow item={item} projects={projects} {...actions} />
      </tbody>
    </table>,
  );
}
