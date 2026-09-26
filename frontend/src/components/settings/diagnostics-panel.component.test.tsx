import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import { DiagnosticsPanel } from "./diagnostics-panel";

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));

describe("DiagnosticsPanel", () => {
  beforeEach(() => {
    vi.mocked(apiDownload).mockReset();
  });

  it("downloads the authenticated redacted report", async () => {
    const user = userEvent.setup();
    vi.mocked(apiDownload).mockResolvedValue({ saved: true, method: "picker" });
    render(<DiagnosticsPanel />);

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));

    expect(apiDownload).toHaveBeenCalledWith("/api/settings/diagnostics", expect.stringMatching(/^aipermission-diagnostics-.*\.json$/));
  });

  it("keeps collection failures visible", async () => {
    const user = userEvent.setup();
    vi.mocked(apiDownload).mockRejectedValue(new Error("Diagnostics unavailable"));
    render(<DiagnosticsPanel />);

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));

    expect(await screen.findByText("Diagnostics unavailable")).toBeVisible();
  });

  it("keeps non-Error failures recoverable with the diagnostics fallback", async () => {
    const user = userEvent.setup();
    vi.mocked(apiDownload).mockRejectedValueOnce(null).mockResolvedValueOnce({ saved: true, method: "picker" });
    render(<DiagnosticsPanel />);
    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(await screen.findByText("Could not download diagnostics.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(screen.queryByText("Could not download diagnostics.")).not.toBeInTheDocument();
    expect(apiDownload).toHaveBeenCalledTimes(2);
  });

  it("disables repeated downloads while pending and ignores completion after unmount", async () => {
    const user = userEvent.setup();
    let resolve!: (_value: Awaited<ReturnType<typeof apiDownload>>) => void;
    vi.mocked(apiDownload).mockReturnValue(new Promise((done) => { resolve = done; }));
    const { unmount } = render(<DiagnosticsPanel />);
    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(screen.getByRole("button", { name: "Preparing diagnostics..." })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Preparing diagnostics..." }));
    expect(apiDownload).toHaveBeenCalledTimes(1);
    unmount();
    await act(async () => resolve({ saved: true, method: "picker" }));
  });
});
