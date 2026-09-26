import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { FieldWithAction, Input } from "./form";

it("labels only the input and keeps the accessory action outside its label", async () => {
  const user = userEvent.setup();
  const onVerify = vi.fn();
  render(
    <FieldWithAction htmlFor="test-endpoint" label="Endpoint" action={<button onClick={onVerify}>Verify endpoint</button>}>
      <Input id="test-endpoint" defaultValue="example.test" />
    </FieldWithAction>,
  );
  const input = screen.getByRole("textbox", { name: "Endpoint" });
  const action = screen.getByRole("button", { name: "Verify endpoint" });
  expect(screen.getByLabelText("Endpoint")).toBe(input);
  expect(action.closest("label")).toBeNull();
  await user.click(screen.getByText("Endpoint", { selector: "label" }));
  expect(input).toHaveFocus();
  expect(onVerify).not.toHaveBeenCalled();
  await user.click(action);
  expect(onVerify).toHaveBeenCalledOnce();
  expect(action).toHaveFocus();
  expect(input).toHaveValue("example.test");
});
