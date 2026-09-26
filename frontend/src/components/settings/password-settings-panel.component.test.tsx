import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../lib/api";
import { PasswordSettingsPanel } from "./password-settings-panel";

vi.mock("../../lib/api", () => ({ apiPost: vi.fn() }));
beforeEach(() => {
  vi.mocked(apiPost).mockReset();
});

function fillForm(password = "NewDatabasePass123") {
  fireEvent.change(screen.getByLabelText("Current password"), { target: { value: "OldDatabasePass123" } });
  fireEvent.change(screen.getByLabelText("New password"), { target: { value: password } });
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: password } });
}

it("requires a strong different password and matching confirmation", () => {
  render(<PasswordSettingsPanel />);
  const submit = screen.getByRole("button", { name: "Change password" });
  expect(submit).toBeDisabled();
  fillForm("short");
  expect(submit).toBeDisabled();
  fillForm("OldDatabasePass123");
  expect(submit).toBeDisabled();
  fillForm();
  expect(submit).toBeEnabled();
  fireEvent.change(screen.getByLabelText("Confirm new password"), { target: { value: "OtherDatabasePass123" } });
  expect(submit).toBeDisabled();
});

it("posts the three fields once and clears them only after success", async () => {
  vi.mocked(apiPost).mockResolvedValueOnce({ ok: true });
  render(<PasswordSettingsPanel />);
  fillForm();
  await userEvent.click(screen.getByRole("button", { name: "Change password" }));
  expect(apiPost).toHaveBeenCalledWith("/api/databases/change-password", {
    current_password: "OldDatabasePass123",
    new_password: "NewDatabasePass123",
    confirm_password: "NewDatabasePass123",
  });
  expect(await screen.findByText(/Database password changed/)).toBeInTheDocument();
  expect(screen.getByLabelText("Current password")).toHaveValue("");
  expect(screen.getByLabelText("New password")).toHaveValue("");
});

it("retains entered values after failure so a user can retry", async () => {
  vi.mocked(apiPost).mockRejectedValueOnce(new Error("Incorrect current password"));
  render(<PasswordSettingsPanel />);
  fillForm();
  await userEvent.click(screen.getByRole("button", { name: "Change password" }));
  expect(await screen.findByText("Incorrect current password")).toBeInTheDocument();
  expect(screen.getByLabelText("Current password")).toHaveValue("OldDatabasePass123");
  expect(screen.getByRole("button", { name: "Change password" })).toBeEnabled();
});
