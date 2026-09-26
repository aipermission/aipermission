import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ComponentProps, MouseEvent } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiDownload } from "../../lib/api";
import type { Button } from "../ui/button";
import { DiagnosticsPanel } from "./diagnostics-panel";

const { observeButton, observeClick } = vi.hoisted(() => ({
  observeButton: vi.fn<(_props: ComponentProps<typeof Button>) => void>(),
  observeClick: vi.fn<(_event: MouseEvent<HTMLButtonElement>) => void>(),
}));

vi.mock("../../lib/api", () => ({ apiDownload: vi.fn() }));
vi.mock("../ui/button", async (importOriginal) => {
  const original = await importOriginal<typeof import("../ui/button")>();
  return {
    ...original,
    Button: (props: ComponentProps<typeof Button>) => {
      observeButton(props);
      return (
        <original.Button
          {...props}
          onClick={(event) => {
            observeClick(event);
            return props.onClick?.(event);
          }}
        />
      );
    },
  };
});

describe("DiagnosticsPanel", () => {
  beforeEach(() => {
    vi.mocked(apiDownload).mockReset();
    observeButton.mockClear();
    observeClick.mockClear();
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

  it("clears the previous error during retry and guards the pending download callback", async () => {
    const user = userEvent.setup();
    let resolve!: (_value: Awaited<ReturnType<typeof apiDownload>>) => void;
    const pending = new Promise<Awaited<ReturnType<typeof apiDownload>>>((done) => {
      resolve = done;
    });
    vi.mocked(apiDownload)
      .mockRejectedValueOnce(new Error("Diagnostics unavailable"))
      .mockReturnValueOnce(pending)
      .mockResolvedValueOnce({ saved: true, method: "picker" });
    render(<DiagnosticsPanel />);
    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(await screen.findByText("Diagnostics unavailable")).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(screen.queryByText("Diagnostics unavailable")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Preparing diagnostics..." })).toBeDisabled();

    // Exercise the callback contract even when a caller bypasses the disabled button.
    const pendingClick = observeButton.mock.lastCall?.[0].onClick;
    const clickEvent = observeClick.mock.lastCall?.[0];
    if (!pendingClick || !clickEvent) throw new Error("Expected a download callback and click event");
    await act(async () => pendingClick(clickEvent));

    expect(apiDownload).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("button", { name: "Preparing diagnostics..." })).toBeDisabled();
    await act(async () => resolve({ saved: true, method: "picker" }));
    expect(screen.getByRole("button", { name: "Download diagnostics" })).toBeEnabled();
    expect(screen.queryByText("Diagnostics unavailable")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(apiDownload).toHaveBeenCalledTimes(3);
    expect(screen.getByRole("button", { name: "Download diagnostics" })).toBeEnabled();
  });

  it("returns to idle after a canceled save and allows another download", async () => {
    const user = userEvent.setup();
    vi.mocked(apiDownload)
      .mockResolvedValueOnce({ saved: false, canceled: true, method: "picker" })
      .mockResolvedValueOnce({ saved: true, method: "anchor" });
    render(<DiagnosticsPanel />);

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));

    expect(screen.getByRole("button", { name: "Download diagnostics" })).toBeEnabled();
    expect(screen.queryByText("Preparing diagnostics...")).not.toBeInTheDocument();
    expect(screen.queryByText("Could not download diagnostics.")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(apiDownload).toHaveBeenCalledTimes(2);
    expect(screen.getByRole("button", { name: "Download diagnostics" })).toBeEnabled();
  });

  it("disables repeated downloads while pending and ignores completion after unmount", async () => {
    const user = userEvent.setup();
    let resolve!: (_value: Awaited<ReturnType<typeof apiDownload>>) => void;
    vi.mocked(apiDownload).mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const { unmount } = render(<DiagnosticsPanel />);
    await user.click(screen.getByRole("button", { name: "Download diagnostics" }));
    expect(screen.getByRole("button", { name: "Preparing diagnostics..." })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "Preparing diagnostics..." }));
    expect(apiDownload).toHaveBeenCalledTimes(1);
    unmount();
    await act(async () => resolve({ saved: true, method: "picker" }));
  });
});
