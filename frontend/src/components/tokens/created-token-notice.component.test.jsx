import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useState } from "react";
import { describe, expect, it } from "vitest";
import { CreatedTokenNotice } from "./created-token-notice";

function NoticeHarness() {
  const [token, setToken] = useState({ name: "maintenance", token: "aip_generated_secret" });
  return <CreatedTokenNotice token={token} onDismiss={() => setToken(null)} />;
}

describe("CreatedTokenNotice", () => {
  it("clears the one-time token state when dismissed", async () => {
    const user = userEvent.setup();
    render(<NoticeHarness />);

    expect(screen.getByText("maintenance token created.")).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Dismiss generated token" }));
    expect(screen.queryByText("maintenance token created.")).not.toBeInTheDocument();
  });
});
