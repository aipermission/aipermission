import { useState } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { DatabaseSwitchDialog } from "./database-switch-dialog.tsx";
import type { useDatabaseLifecycle } from "./use-database-lifecycle.ts";

const submit = vi.fn();
function Harness() {
  const [state, setState] = useState<ReturnType<typeof useDatabaseLifecycle>["switchDialog"]>({ open: true, database_id: "one", password: "", state: "idle", error: null });
  return <DatabaseSwitchDialog state={state} onChange={setState} onClose={() => {}} onSubmit={submit} databaseStatus={{ database_id: "one", databases: [
    { id: "one", name: "Current", unlocked: true },
    { id: "two", name: "Locked", unlocked: false },
    { id: "three", name: "Ready", unlocked: true },
  ] }} />;
}

it("only asks for a password when the selected database is locked", async () => {
  const user = userEvent.setup();
  render(<Harness />);
  expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  expect(screen.queryByLabelText("Database password")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Locked two" }));
  const password = screen.getByLabelText("Database password");
  expect(password).toBeRequired();
  await user.type(password, "private");
  await user.click(screen.getByRole("button", { name: "Ready three" }));
  expect(screen.getByRole("button", { name: "Switch" })).toBeEnabled();
  expect(screen.queryByLabelText("Database password")).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Locked two" }));
  expect(screen.getByLabelText("Database password")).toHaveValue("");
});
