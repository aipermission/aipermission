import { fireEvent, render, screen } from "@testing-library/react";
import type { ComponentProps } from "react";
import { describe, expect, it, vi } from "vitest";
import { MessagesDialog } from "./messages-dialog.tsx";
import type { RuntimeMessage } from "../../lib/gateway-contracts/activity-resource-contracts.ts";

function message(id: number, tokenID: number, createdAt: string): RuntimeMessage {
  return { id, token_id: tokenID, direction: "ai_to_user", message: `Note ${id}`, created_at: createdAt };
}

function props(overrides: Partial<ComponentProps<typeof MessagesDialog>> = {}): ComponentProps<typeof MessagesDialog> {
  return {
    open: true,
    target: { name: "My target" },
    tokens: [{ id: 5, name: "Agent" }],
    tokenID: "5",
    state: { state: "ready", data: [], error: null },
    text: "A note",
    onTokenChange: vi.fn(),
    onTextChange: vi.fn(),
    onSubmit: vi.fn().mockResolvedValue(undefined),
    onRefresh: vi.fn().mockResolvedValue(undefined),
    onClose: vi.fn(),
    ...overrides,
  };
}

describe("MessagesDialog", () => {
  it("filters by token and orders messages by timestamp then identity without mutating the source", () => {
    const data = [message(3, 5, "2026-09-26"), message(2, 5, "2026-09-25"), message(1, 5, "2026-09-25"), message(4, 6, "2026-09-24")];
    render(<MessagesDialog {...props({ state: { state: "ready", data, error: null } })} />);
    expect(screen.queryByText("Note 4")).not.toBeInTheDocument();
    expect(screen.getAllByText(/^Note/).map((element) => element.textContent)).toEqual(["Note 1", "Note 2", "Note 3"]);
    expect(data.map((item) => item.id)).toEqual([3, 2, 1, 4]);
  });

  it.each([
    { tokenID: "", text: "note", state: "ready" as const },
    { tokenID: "5", text: "  ", state: "ready" as const },
    { tokenID: "5", text: "note", state: "sending" as const },
  ])("disables submission when the token, draft or sending state prevents it ($state)", ({ tokenID, text, state }) => {
    render(<MessagesDialog {...props({ tokenID, text, state: { state, data: [], error: null } })} />);
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
  });

  it("routes edits and refresh to their owners and displays load errors", () => {
    const value = props({ state: { state: "error", data: [], error: "Unavailable" } });
    render(<MessagesDialog {...value} />);
    fireEvent.change(screen.getByRole("textbox", { name: "Message to AI" }), { target: { value: "New note" } });
    fireEvent.click(screen.getByRole("button", { name: "Refresh messages" }));
    expect(value.onTextChange).toHaveBeenCalledWith("New note");
    expect(value.onRefresh).toHaveBeenCalledOnce();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });

  it("shows the empty-token notice and preserves an invalid timestamp as display data", () => {
    render(<MessagesDialog {...props({ tokens: [], tokenID: "", state: { state: "ready", data: [message(1, 5, "unknown time")], error: null } })} />);
    expect(screen.getByText("No token has access to this target.")).toBeInTheDocument();
    expect(screen.getByText(/unknown time/)).toBeInTheDocument();
  });
});
