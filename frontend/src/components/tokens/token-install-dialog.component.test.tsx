import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { TokenInstallDialog, type TokenInstallState } from "./token-install-dialog";

function InstallDialog({ initialState }: { initialState: TokenInstallState }) {
  const [state, setState] = useState(initialState);
  return <TokenInstallDialog state={state} onChange={setState} onClose={vi.fn()} />;
}

describe("TokenInstallDialog", () => {
  it("switches from portable manual config to a token-free setup command", async () => {
    const user = userEvent.setup();
    render(<InstallDialog initialState={{ open: true, token: { name: "My Project", token: "fixture-token" } }} />);

    expect(screen.getByText(/"AIPERMISSION_API_TOKEN": "fixture-token"/)).toBeVisible();
    expect(screen.getByText("aipermission-my-project")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Custom / copy-paste" }));
    expect(screen.getByText(/--provider custom/)).toBeVisible();
    expect(screen.getByText(/--print/)).toBeVisible();
    expect(screen.queryByText(/"AIPERMISSION_API_TOKEN": "fixture-token"/)).not.toBeInTheDocument();
  });

  it("does not offer a setup command when the token value is unavailable", () => {
    render(<InstallDialog initialState={{ open: true, token: { name: "Existing token" } }} />);

    expect(screen.getByText(/not available for copy/)).toBeVisible();
    expect(screen.queryByRole("button", { name: "Custom / copy-paste" })).not.toBeInTheDocument();
  });
});
