import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { CopyButton } from "./copy-button";

describe("CopyButton", () => {
  it("copies the requested value and reports success", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    const onCopied = vi.fn();
    render(<CopyButton value="target-ref" onCopied={onCopied} />);

    await user.click(screen.getByRole("button", { name: "Copy" }));

    expect(writeText).toHaveBeenCalledWith("target-ref");
    expect(onCopied).toHaveBeenCalledOnce();
    expect(screen.getByRole("button", { name: "Copy" })).toHaveAttribute("title", "Copied");
  });
});
