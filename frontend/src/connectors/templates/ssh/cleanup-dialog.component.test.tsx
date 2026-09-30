import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, expect, it, vi } from "vitest";
import { apiPost } from "../../../lib/api";
import { cleanupFixture, cleanupTargetID } from "./cleanup-test-fixtures";
import { SSHCleanupDialog } from "./cleanup-dialog";
import { SSHConnectorRowActionsTemplate } from "./list-item";
import { SSHConnectorOperationsTemplate } from "./operations";
import type { SSHModelTarget } from "./model-types";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: () => "workspace-a" }));
const target: SSHModelTarget = { id: cleanupTargetID, name: "Cleanup target", connector_kind: "ssh" };

beforeEach(() => {
  vi.mocked(apiPost).mockReset().mockResolvedValue(cleanupFixture().wire);
});

async function fillProof() {
  const user = userEvent.setup();
  const { coverage, submission } = cleanupFixture();
  for (const [index, proof] of coverage.entries()) {
    await user.selectOptions(screen.getAllByLabelText("External verification method")[index], proof.method);
    await user.type(screen.getAllByLabelText("Location evidence")[index], proof.reason);
    await user.click(screen.getAllByRole("checkbox")[index]);
  }
  await user.type(screen.getByLabelText("Decision reason"), submission.reason);
  return user;
}

async function opened() {
  const onClose = vi.fn();
  const view = render(<SSHCleanupDialog target={target} onClose={onClose} />);
  await screen.findByRole("button", { name: "Record external absence" });
  return { ...view, onClose };
}

it("shows every original and current location without prechecking absence", async () => {
  await opened();
  const { wire } = cleanupFixture();
  const subjects = wire.records[0].choices[0].subjects;
  for (const subject of subjects) {
    const group = screen.getByRole("group", { name: `${subject.username}@${subject.host}:${subject.port}` });
    expect(within(group).getByRole("checkbox")).not.toBeChecked();
    expect(within(group).getByLabelText("Location evidence")).toHaveValue("");
    expect(within(group).getByTitle("Copy key fingerprint")).toBeEnabled();
    expect(within(group).getAllByText(subject.key_fingerprint)).toHaveLength(2);
  }
  expect(screen.getByText(/Recording evidence does not remove a remote key/)).toBeVisible();
  expect(screen.getByLabelText("Selected identity")).toHaveDisplayValue(
    `operator@cleanup.example.test:22 / ${wire.records[0].choices[0].digest.slice(0, 12)}`,
  );
});

it("keeps persisted decisions inspectable separately from fresh confirmation fields", async () => {
  await opened();
  const user = userEvent.setup();
  await user.click(screen.getByText("Original identity"));
  const history = screen.getByRole("region", { name: "Recorded cleanup evidence" });
  const original = screen.getByRole("textbox", { name: "Original cleanup identity JSON" }) as HTMLTextAreaElement;
  expect(JSON.parse(original.value).host).toBe("retired.example.test");
  await user.click(screen.getByText("Decision 1: operator@retired.example.test:22"));
  const reason = cleanupFixture().wire.records[0].entry.record.attestations[0].reason;
  expect(within(history).getByText(reason)).toBeVisible();
  const proof = within(history).getByRole("textbox", { name: "Recorded decision 1 JSON" }) as HTMLTextAreaElement;
  expect(JSON.parse(proof.value).reason).toBe(reason);
  expect(proof.readOnly).toBe(true);
  expect(within(history).getByTitle("Copy recorded decision 1")).toBeEnabled();
  await user.click(within(history).getByTitle("Copy recorded decision 1"));
  await waitFor(async () =>
    expect(JSON.parse(await navigator.clipboard.readText())).toEqual(cleanupFixture().wire.records[0].entry.record.attestations[0]),
  );
  screen.getAllByRole("checkbox").forEach((checkbox) => expect(checkbox).not.toBeChecked());
  expect(screen.getByLabelText("Decision reason")).toHaveValue("");
});

it("distinguishes a confirmed journal with no historical operator decisions", async () => {
  const { wire } = cleanupFixture();
  wire.records[0].entry.record.status = "confirmed";
  wire.records[0].entry.record.attestations = [];
  vi.mocked(apiPost).mockResolvedValueOnce(wire);
  await opened();
  expect(screen.getByText("No recorded operator decisions.")).toBeVisible();
});

it("validates before any mutation and requires evidence for every location", async () => {
  await opened();
  const user = userEvent.setup();
  await user.click(screen.getByRole("button", { name: "Record external absence" }));
  expect(await screen.findByText(/Enter a decision reason/)).toBeVisible();
  await user.type(screen.getByLabelText("Decision reason"), "Externally checked");
  await user.click(screen.getByRole("button", { name: "Record external absence" }));
  expect(await screen.findByText(/Confirm absence, an external method/)).toBeVisible();
  expect(apiPost).toHaveBeenCalledOnce();
});

it("disables fields, refresh and close while recording and clears inputs after reloading", async () => {
  const { onClose } = await opened();
  const user = await fillProof();
  let finish!: (_value: unknown) => void;
  const pending = new Promise<unknown>((resolve) => {
    finish = resolve;
  });
  const { wire, acknowledgement, submission } = cleanupFixture();
  vi.mocked(apiPost)
    .mockReturnValueOnce(pending)
    .mockResolvedValueOnce({ ...wire, records: [{ ...wire.records[0], entry: acknowledgement.entry }] });
  await user.click(screen.getByRole("button", { name: "Record external absence" }));
  expect(screen.getByRole("button", { name: "Recording..." })).toBeDisabled();
  expect(screen.getByTitle("Reload cleanup evidence")).toBeDisabled();
  expect(screen.getByRole("button", { name: "Close dialog" })).toBeDisabled();
  expect(screen.getByLabelText("Decision reason")).toBeDisabled();
  screen.getAllByRole("checkbox").forEach((checkbox) => expect(checkbox).toBeDisabled());
  await user.keyboard("{Escape}");
  expect(onClose).not.toHaveBeenCalled();
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-targets/7/operations/key-cleanup-attest",
    submission,
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
  await act(async () => finish(acknowledgement));
  expect(await screen.findByText("Decision recorded. Current cleanup evidence reloaded.")).toBeVisible();
  expect(screen.getByLabelText("Decision reason")).toHaveValue("");
  screen.getAllByRole("checkbox").forEach((checkbox) => expect(checkbox).not.toBeChecked());
  expect(screen.getByLabelText("Cleanup record")).toHaveDisplayValue("Record 41 / attested");
  expect(screen.getByRole("button", { name: "Close dialog" })).toBeEnabled();
});

it("clears previously entered proof when choosing a different server identity", async () => {
  const { wire } = cleanupFixture();
  // Exercise the declared choice selector contract separately from the normal single-current-choice fixture.
  const alternative = cleanupFixture(cleanupTargetID, true).wire.records[0].choices[0];
  wire.records[0].choices.push(alternative);
  vi.mocked(apiPost).mockResolvedValueOnce(wire);
  await opened();
  const user = await fillProof();
  await user.selectOptions(screen.getByLabelText("Selected identity"), alternative.digest);
  expect(screen.getAllByRole("checkbox")).toHaveLength(1);
  expect(screen.getByRole("checkbox")).not.toBeChecked();
  expect(screen.getByLabelText("Location evidence")).toHaveValue("");
  expect(screen.getByLabelText("Decision reason")).toHaveValue("");
  expect(apiPost).toHaveBeenCalledOnce();
});

it("clears previously entered proof when choosing another record", async () => {
  const { wire } = cleanupFixture();
  const extra = structuredClone(wire.records[0]);
  extra.entry.resource_id = 42;
  wire.records.push(extra);
  vi.mocked(apiPost).mockResolvedValue(wire);
  await opened();
  const user = await fillProof();
  await user.selectOptions(screen.getByLabelText("Cleanup record"), "42");
  expect(screen.getByLabelText("Decision reason")).toHaveValue("");
  screen.getAllByRole("checkbox").forEach((checkbox) => expect(checkbox).not.toBeChecked());
});

it("reports a lost reply without claiming success and observes the committed generation", async () => {
  await opened();
  const user = await fillProof();
  const { wire, acknowledgement } = cleanupFixture();
  vi.mocked(apiPost)
    .mockRejectedValueOnce(new Error("Lost reply"))
    .mockResolvedValueOnce({ ...wire, records: [{ ...wire.records[0], entry: acknowledgement.entry }] });
  await user.click(screen.getByRole("button", { name: "Record external absence" }));
  expect(await screen.findByText(/outcome was not confirmed/)).toBeVisible();
  expect(screen.queryByText(/Decision recorded/)).not.toBeInTheDocument();
  expect(screen.getByLabelText("Cleanup record")).toHaveDisplayValue("Record 41 / attested");
  expect(vi.mocked(apiPost).mock.calls.filter(([path]) => path.endsWith("key-cleanup-attest"))).toHaveLength(1);
  expect(screen.getByLabelText("Decision reason")).toHaveValue("");
});

it("keeps a malformed response distinct from an empty valid journal", async () => {
  vi.mocked(apiPost).mockResolvedValueOnce({ ...cleanupFixture().wire, records: null });
  render(<SSHCleanupDialog target={target} onClose={vi.fn()} />);
  expect(await screen.findByText("Invalid SSH cleanup status response.")).toBeVisible();
  expect(screen.queryByText("No recorded key cleanup for this target.")).not.toBeInTheDocument();
  vi.mocked(apiPost).mockResolvedValueOnce({ ...cleanupFixture().wire, records: [] });
  await userEvent.setup().click(screen.getByTitle("Reload cleanup evidence"));
  expect(await screen.findByText("No recorded key cleanup for this target.")).toBeVisible();
  expect(screen.queryByRole("button", { name: "Record external absence" })).not.toBeInTheDocument();
});

it("exposes target-owned reconciliation even with no execution profile", async () => {
  const onOperation = vi.fn();
  render(<SSHConnectorRowActionsTemplate target={target} profile={null} onOperation={onOperation} />);
  await userEvent.setup().click(screen.getByTitle("Reconcile key cleanup"));
  expect(onOperation).toHaveBeenCalledWith({ connector_kind: "ssh", type: "key-cleanup", target, open: true });
  expect(screen.getByTitle("Install key for Cleanup target")).toBeDisabled();
  expect(screen.getByTitle("Check Docker for Cleanup target")).toBeDisabled();
});

it("renders the connector-owned operation and retires it through the shared close callback", async () => {
  const onChange = vi.fn();
  render(
    <SSHConnectorOperationsTemplate
      value={{ connector_kind: "ssh", type: "key-cleanup", target, open: true }}
      credentials={[]}
      onChange={onChange}
      onOperationComplete={vi.fn()}
    />,
  );
  await screen.findByRole("button", { name: "Record external absence" });
  expect(apiPost).toHaveBeenCalledOnce();
  await userEvent.setup().click(screen.getByRole("button", { name: "Close dialog" }));
  expect(onChange).toHaveBeenCalledWith({ open: false, connector_kind: "", type: "", state: "idle", error: null });
});

it("remounts the operation for a different target without retaining checked evidence", async () => {
  const onChange = vi.fn();
  const props = { credentials: [], onChange, onOperationComplete: vi.fn() };
  const view = render(
    <SSHConnectorOperationsTemplate {...props} value={{ connector_kind: "ssh", type: "key-cleanup", target, open: true }} />,
  );
  await screen.findByRole("button", { name: "Record external absence" });
  await fillProof();
  vi.mocked(apiPost).mockResolvedValueOnce({ ...cleanupFixture().wire, target_id: 8 });
  view.rerender(
    <SSHConnectorOperationsTemplate
      {...props}
      value={{ connector_kind: "ssh", type: "key-cleanup", target: { ...target, id: 8, name: "Another target" }, open: true }}
    />,
  );
  await waitFor(() => expect(screen.getByRole("dialog")).toHaveAccessibleName("Reconcile key cleanup for Another target"));
  expect(await screen.findByLabelText("Decision reason")).toHaveValue("");
  screen.getAllByRole("checkbox").forEach((checkbox) => expect(checkbox).not.toBeChecked());
  expect(apiPost).toHaveBeenLastCalledWith(
    "/api/connector-targets/8/operations/key-cleanup-status",
    {},
    expect.objectContaining({ signal: expect.any(AbortSignal) }),
  );
});
