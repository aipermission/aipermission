import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Database } from "lucide-react";
import { expect, it, vi } from "vitest";
import { StructuredSessionEmpty } from "./structured-session-empty";

it("renders connector-owned session guidance and starts the workflow", async () => {
  const user = userEvent.setup();
  const onStart = vi.fn();
  render(
    <StructuredSessionEmpty
      icon={Database}
      title="No active Example session"
      description="Start a bounded Example session."
      buttonLabel="Start Example session"
      onStart={onStart}
      panelClass="panel"
      mutedClass="muted"
      footer={<div>example endpoint</div>}
    />,
  );
  expect(screen.getByRole("heading", { name: "No active Example session" })).toBeInTheDocument();
  expect(screen.getByText("example endpoint")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Start Example session" }));
  expect(onStart).toHaveBeenCalledOnce();
});
