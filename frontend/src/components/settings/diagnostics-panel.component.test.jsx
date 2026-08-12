import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import { DiagnosticsPanel } from "./diagnostics-panel";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));

describe("DiagnosticsPanel", () => {
  beforeEach(() => {
    apiDownload.mockReset();
  });

  it("downloads the authenticated redacted report", async () => {
    const user = userEvent.setup();
    apiDownload.mockResolvedValue({ saved: true });
    render(<DiagnosticsPanel />);

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));

    expect(apiDownload).toHaveBeenCalledWith("/api/settings/diagnostics", expect.stringMatching(/^aipermission-diagnostics-.*\.json$/));
  });

  it("keeps collection failures visible", async () => {
    const user = userEvent.setup();
    apiDownload.mockRejectedValue(new Error("Diagnostics unavailable"));
    render(<DiagnosticsPanel />);

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));

    expect(await screen.findByText("Diagnostics unavailable")).toBeVisible();
  });
});
