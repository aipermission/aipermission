import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it } from "vitest";
import { OptionalSecretField } from "./optional-secret-field";

function Harness({ editing = true }: { editing?: boolean }) {
  const [value, setValue] = useState("");
  const [clear, setClear] = useState(false);
  return (
    <OptionalSecretField
      label="New password"
      value={value}
      editing={editing}
      clear={clear}
      placeholder="Keep current password"
      onChange={setValue}
      onClear={setClear}
    />
  );
}

it("requires an explicit edit-only clear intent and allows it to be undone", async () => {
  const user = userEvent.setup();
  const { rerender } = render(<Harness />);
  const input = screen.getByLabelText("New password");
  await user.type(input, "replacement");
  await user.click(screen.getByRole("checkbox", { name: "Remove stored password" }));
  expect(input).toBeDisabled();
  await user.click(screen.getByRole("checkbox", { name: "Remove stored password" }));
  expect(input).toBeEnabled();
  expect(input).toHaveValue("replacement");
  rerender(<Harness editing={false} />);
  expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
});
