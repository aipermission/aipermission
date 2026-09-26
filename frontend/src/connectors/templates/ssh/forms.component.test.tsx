import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { expect, it, vi } from "vitest";
import { SSHConnectorFormTemplate } from "./form";
import { SSHCredentialFormTemplate } from "./credential-form";
import { SSHConnectorRowActionsTemplate } from "./list-item";
import { emptyForm } from "./model";
import type { SSHForm, SSHKeyForm, SSHTarget, SSHProfile } from "./form-types";

it("keeps SSH connection fields and appliance startup settings local to its template", async () => {
  const form: SSHForm = emptyForm();
  const onChange = vi.fn();
  const key = { id: 7, name: "main", key_type: "ed25519", install_command: "install-key" };
  render(
    <MemoryRouter>
      <SSHConnectorFormTemplate form={form} credentials={[key]} activeCredential={key} onChange={onChange} />
    </MemoryRouter>,
  );
  expect(screen.getByText("main · ed25519")).toBeInTheDocument();
  expect(screen.getAllByText("install-key").length).toBeGreaterThan(0);
  fireEvent.change(screen.getByRole("textbox", { name: "Host" }), { target: { value: "203.0.113.10" } });
  expect(onChange).toHaveBeenCalledWith("host", "203.0.113.10");
  await userEvent.click(screen.getByRole("checkbox", { name: "I will install the key later" }));
  expect(onChange).toHaveBeenCalledWith("setup_later", true);
  fireEvent.change(screen.getByRole("textbox", { name: /Startup input after connect/ }), { target: { value: "q\n" } });
  expect(onChange).toHaveBeenCalledWith("startup_input_after_connect", "q\n");
});

it("directs users to credentials when no gateway key is available", () => {
  render(
    <MemoryRouter>
      <SSHConnectorFormTemplate form={emptyForm()} credentials={[]} activeCredential={null} onChange={vi.fn()} />
    </MemoryRouter>,
  );
  expect(screen.getByRole("link", { name: "Open Credentials" })).toHaveAttribute("href", "/credentials");
});

const credentialProps = () => ({
  mode: "generate" as const,
  form: { name: "main", key_type: "ed25519" } satisfies SSHKeyForm,
  importForm: { name: "imported", private_key: "", passphrase: "" },
  state: { state: "idle" },
  onModeChange: vi.fn(),
  onFormChange: vi.fn(),
  onImportFormChange: vi.fn(),
  onReadImportFile: vi.fn(),
  onCreate: vi.fn((event) => event.preventDefault()),
  onImport: vi.fn((event) => event.preventDefault()),
  onUpdate: vi.fn((event) => event.preventDefault()),
});

it("keeps generation, import, and label-only editing distinct", async () => {
  const props = credentialProps();
  const { rerender } = render(<SSHCredentialFormTemplate {...props} />);
  await userEvent.click(screen.getByRole("button", { name: "rsa" }));
  expect(props.onFormChange).toHaveBeenCalledWith({ name: "main", key_type: "rsa" });
  await userEvent.click(screen.getByRole("button", { name: "Generate ed25519 credential" }));
  expect(props.onCreate).toHaveBeenCalledOnce();
  await userEvent.click(screen.getByRole("button", { name: "Import" }));
  expect(props.onModeChange).toHaveBeenCalledWith("import");
  rerender(
    <SSHCredentialFormTemplate {...props} mode="import" importForm={{ name: "imported", private_key: "test-fixture", passphrase: "" }} />,
  );
  await userEvent.click(screen.getByRole("button", { name: "Import credential" }));
  expect(props.onImport).toHaveBeenCalledOnce();
  rerender(<SSHCredentialFormTemplate {...props} formMode="edit" />);
  expect(screen.queryByRole("textbox", { name: "Private key" })).not.toBeInTheDocument();
  await userEvent.click(screen.getByRole("button", { name: "Save SSH credential" }));
  expect(props.onUpdate).toHaveBeenCalledOnce();
});

it("dispatches only template operations for the chosen target profile", async () => {
  const target: SSHTarget = { id: 3, connector_kind: "ssh", name: "worker-1" };
  const profile: SSHProfile = { id: 7, public: { username: "root" } };
  const onOperation = vi.fn();
  const { rerender } = render(<SSHConnectorRowActionsTemplate target={target} profile={profile} onOperation={onOperation} />);
  await userEvent.click(screen.getByRole("button", { name: "Install key for worker-1" }));
  expect(onOperation).toHaveBeenCalledWith({ connector_kind: "ssh", type: "install", target, profile, open: true });
  await userEvent.click(screen.getByRole("button", { name: "Check Docker for worker-1" }));
  expect(onOperation).toHaveBeenLastCalledWith({ connector_kind: "ssh", type: "docker-check", target, profile, open: true, state: "idle" });
  rerender(<SSHConnectorRowActionsTemplate target={target} profile={null} onOperation={onOperation} />);
  expect(screen.getByRole("button", { name: "Install key for worker-1" })).toBeDisabled();
});
