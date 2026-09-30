import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { sshConsoleRecovery } from "./console-recovery";
import { apiPost as realPost } from "../../../lib/api";
import { useState } from "react";
import type { ConsoleOperation, ConsoleOperationSlotProps } from "../../../components/console/console-connector-view-types";

vi.mock("../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: () => "recovery-workspace" }));
const apiPost = vi.mocked(realPost);
const runtimeTarget = { id: 19, name: "My runtime", connector_kind: "ssh", target_id: 3, profile_id: 7 };
const hostKey = {
  host: "endpoint",
  hostname: "endpoint",
  port: 22,
  public_key: "public-key",
  fingerprint_sha256: "SHA256:example",
  key_type: "ed25519",
};
const error = { status: 409, data: { code: "unknown_ssh_host_key", host_key: hostKey } };
const context = { operation: "new-session", target: runtimeTarget };

beforeEach(() => {
  apiPost.mockReset();
});

function RecoveryHarness({
  operation,
  onComplete,
}: {
  operation: ConsoleOperation;
  onComplete: ConsoleOperationSlotProps["onOperationComplete"];
}) {
  const [value, setValue] = useState<ConsoleOperation>(operation);
  return <sshConsoleRecovery.Operations value={value} credentials={[]} onChange={setValue} onOperationComplete={onComplete} />;
}

it("uses the native fingerprint dialog and resumes the exact independent runtime after approval", async () => {
  const operation = sshConsoleRecovery.operationFromError(error, context);
  if (!operation) throw new Error("Missing native recovery");
  const onComplete = vi.fn();
  apiPost.mockResolvedValue({ ok: true });
  render(<RecoveryHarness operation={operation} onComplete={onComplete} />);
  expect(await screen.findByText("SHA256:example")).toBeInTheDocument();
  fireEvent.click(await screen.findByRole("button", { name: "Approve fingerprint" }));
  await waitFor(() => expect(onComplete).toHaveBeenCalledWith({ startConsoleSession: true }, { connector_kind: "ssh", runtimeTarget }));
  expect(onComplete.mock.calls[0][1].runtimeTarget).toBe(runtimeTarget);
  expect(apiPost).toHaveBeenCalledWith(
    "/api/connectors/ssh/host-keys/approve",
    { host: "endpoint", port: 22, public_key: "public-key", replace: false },
    { signal: expect.any(AbortSignal) },
  );
  await waitFor(() => expect(screen.queryByText("SHA256:example")).not.toBeInTheDocument());
});

it("keeps a fingerprint approval failure visible and allows retry", async () => {
  const operation = sshConsoleRecovery.operationFromError(error, context);
  if (!operation) throw new Error("Missing native recovery");
  const onComplete = vi.fn();
  apiPost.mockRejectedValueOnce(new Error("approval failed")).mockResolvedValueOnce({ ok: true });
  render(<RecoveryHarness operation={operation} onComplete={onComplete} />);
  fireEvent.click(await screen.findByRole("button", { name: "Approve fingerprint" }));
  await waitFor(() => expect(screen.getByText("approval failed")).toBeInTheDocument());
  expect(onComplete).not.toHaveBeenCalled();
  fireEvent.click(await screen.findByRole("button", { name: "Approve fingerprint" }));
  await waitFor(() => expect(onComplete).toHaveBeenCalledTimes(1));
});

it("does not capture unrelated errors, operations, foreign connectors, or unnamed/invalid runtimes", () => {
  expect(sshConsoleRecovery.operationFromError(new Error("offline"), context)).toBeNull();
  expect(sshConsoleRecovery.operationFromError(error, { ...context, operation: "test" })).toBeNull();
  for (const target of [
    { ...runtimeTarget, id: 0 },
    { ...runtimeTarget, id: -1 },
    { ...runtimeTarget, id: 1.5 },
    { ...runtimeTarget, connector_kind: "example" },
    { id: 19, connector_kind: "ssh" },
  ])
    expect(sshConsoleRecovery.operationFromError(error, { ...context, target })).toBeNull();
});

it("aborts native fingerprint approval on unmount and does not restart its old runtime", async () => {
  let resolve!: (_value: unknown) => void;
  apiPost.mockReturnValueOnce(
    new Promise<unknown>((done) => {
      resolve = done;
    }),
  );
  const operation = sshConsoleRecovery.operationFromError(error, context);
  if (!operation) throw new Error("Missing native recovery");
  const onComplete = vi.fn();
  const { unmount } = render(<RecoveryHarness operation={operation} onComplete={onComplete} />);
  fireEvent.click(await screen.findByRole("button", { name: "Approve fingerprint" }));
  const signal = apiPost.mock.calls[0][2]?.signal;
  expect(signal?.aborted).toBe(false);
  unmount();
  expect(signal?.aborted).toBe(true);
  await act(async () => {
    resolve({ ok: true });
  });
  expect(onComplete).not.toHaveBeenCalled();
});

it("retires a pending native approval when another recovery replaces it", async () => {
  let resolve!: (_value: unknown) => void;
  apiPost.mockReturnValueOnce(
    new Promise<unknown>((done) => {
      resolve = done;
    }),
  );
  const first = sshConsoleRecovery.operationFromError(error, context);
  const second = sshConsoleRecovery.operationFromError(
    { ...error, data: { ...error.data, host_key: { ...hostKey, fingerprint_sha256: "SHA256:next" } } },
    context,
  );
  if (!first || !second) throw new Error("Missing native recovery");
  const onComplete = vi.fn();
  const props = { credentials: [], onChange: vi.fn(), onOperationComplete: onComplete };
  const { rerender } = render(<sshConsoleRecovery.Operations {...props} value={first} />);
  fireEvent.click(await screen.findByRole("button", { name: "Approve fingerprint" }));
  const signal = apiPost.mock.calls[0][2]?.signal;
  rerender(<sshConsoleRecovery.Operations {...props} value={second} />);
  expect(signal?.aborted).toBe(true);
  expect(screen.getByText("SHA256:next")).toBeInTheDocument();
  await act(async () => {
    resolve({ ok: true });
  });
  expect(onComplete).not.toHaveBeenCalled();
});
