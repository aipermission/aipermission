import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { useState } from "react";
import { ConnectorSlotBoundary } from "./connector-slot-boundary";

it.each(["console", "toolbar"] as const)(
  "contains a failing %s slot without exposing exception payloads and permits explicit retry",
  async (slot) => {
    vi.spyOn(console, "error").mockImplementation(() => {});
    let fail = true;
    function NativeSlot() {
      if (fail) throw new Error("opaque exception payload must not be shown");
      return <p>Recovered native slot</p>;
    }
    render(
      <>
        <p>Surrounding navigation</p>
        <ConnectorSlotBoundary slot={slot}>
          <NativeSlot />
        </ConnectorSlotBoundary>
      </>,
    );
    expect(screen.getByText("Surrounding navigation")).toBeInTheDocument();
    expect(screen.getByText(`Connector ${slot} unavailable.`)).toBeInTheDocument();
    expect(screen.queryByText(/opaque exception payload/)).not.toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: `Retry ${slot}` }));
    expect(screen.getByText(`Connector ${slot} unavailable.`)).toBeInTheDocument();
    fail = false;
    await user.click(screen.getByRole("button", { name: `Retry ${slot}` }));
    expect(screen.getByText("Recovered native slot")).toBeInTheDocument();
  },
);

it("starts a clean boundary when the selected target/profile identity changes", () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  function InvalidSlot(): never {
    throw new Error("invalid config");
  }
  const view = render(
    <ConnectorSlotBoundary key="target:3:11" slot="console">
      <InvalidSlot />
    </ConnectorSlotBoundary>,
  );
  expect(screen.getByText("Connector console unavailable.")).toBeInTheDocument();
  view.rerender(
    <ConnectorSlotBoundary key="target:3:12" slot="console">
      <p>Other profile</p>
    </ConnectorSlotBoundary>,
  );
  expect(screen.getByText("Other profile")).toBeInTheDocument();
  expect(screen.queryByText("Connector console unavailable.")).not.toBeInTheDocument();
});

it("recovers after a saved-target update without remounting a healthy slot on later updates", async () => {
  vi.spyOn(console, "error").mockImplementation(() => {});
  let fail = true;
  function NativeSlot() {
    const [draft, setDraft] = useState("");
    if (fail) throw new Error("invalid config");
    return <input aria-label="Native draft" value={draft} onChange={(event) => setDraft(event.target.value)} />;
  }
  const view = render(
    <ConnectorSlotBoundary resetKey="initial" slot="console">
      <NativeSlot />
    </ConnectorSlotBoundary>,
  );
  expect(screen.getByText("Connector console unavailable.")).toBeInTheDocument();
  fail = false;
  view.rerender(
    <ConnectorSlotBoundary resetKey="corrected" slot="console">
      <NativeSlot />
    </ConnectorSlotBoundary>,
  );
  const user = userEvent.setup();
  await user.type(screen.getByRole("textbox", { name: "Native draft" }), "Keep this draft");
  view.rerender(
    <ConnectorSlotBoundary resetKey="metadata-updated" slot="console">
      <NativeSlot />
    </ConnectorSlotBoundary>,
  );
  expect(screen.getByRole("textbox", { name: "Native draft" })).toHaveValue("Keep this draft");
});
