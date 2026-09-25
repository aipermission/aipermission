import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ActionMenu } from "./action-menu";
import { DateTimePicker } from "./date-time-picker";

describe("typed interaction controls", () => {
  it("moves through menu items and selects the focused item", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    render(
      <ActionMenu
        trigger="Add connector"
        label="Connector types"
        items={["ssh", "postgres"]}
        renderItem={(item) => item}
        onSelect={onSelect}
      />,
    );

    await user.click(screen.getByRole("button", { name: "Add connector" }));
    expect(screen.getByRole("menuitem", { name: "ssh" })).toHaveFocus();
    await user.keyboard("{ArrowDown}{Enter}");
    expect(onSelect).toHaveBeenCalledWith("postgres");
    expect(screen.queryByRole("menu")).not.toBeInTheDocument();
  });

  it("preserves the other date-time segment and clears the value", () => {
    const onChange = vi.fn();
    render(<DateTimePicker value="2026-09-25T14:30" onChange={onChange} />);

    fireEvent.change(screen.getByLabelText("Expiry date"), { target: { value: "2026-09-26" } });
    expect(onChange).toHaveBeenLastCalledWith("2026-09-26T14:30");
    fireEvent.change(screen.getByLabelText("Expiry time"), { target: { value: "15:45" } });
    expect(onChange).toHaveBeenLastCalledWith("2026-09-25T15:45");
    fireEvent.click(screen.getByRole("button", { name: "Clear expiry" }));
    expect(onChange).toHaveBeenLastCalledWith("");
  });
});
