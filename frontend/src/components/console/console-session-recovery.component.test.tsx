import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, expect, it, vi } from "vitest";
import { useConsoleConnectorView } from "./use-console-connector-view";
import { useConsoleWorkspaceSession } from "./use-console-workspace-session";
import { apiPost as realPost } from "../../lib/api";
import type { ConsoleRuntimeTarget } from "../use-gateway-resources";

vi.mock("../../lib/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("../../lib/api")>()), apiPost: vi.fn() }));
const apiPost = vi.mocked(realPost);
type Props = Parameters<typeof useConsoleWorkspaceSession>[0];
type Runtime = ConsoleRuntimeTarget & { connector_kind: string; connector_ref: string };

const original: Runtime = { id: 19, name: "Original", connector_kind: "ssh", connector_ref: "ssh:3:7", target_id: 3, profile_id: 7 };
const other: Runtime = { id: 29, name: "Other", connector_kind: "ssh", connector_ref: "ssh:5:11", target_id: 5, profile_id: 11 };
const fingerprintError = {
  status: 409,
  data: {
    code: "changed_ssh_host_key",
    host_key: {
      host: "endpoint",
      hostname: "endpoint",
      port: 22,
      public_key: "public-key",
      fingerprint_sha256: "SHA256:new",
      key_type: "ed25519",
      changed: true,
      existing_fingerprints: ["SHA256:old"],
    },
  },
};

beforeEach(() => {
  apiPost.mockReset();
});

function Workspace({ runtime, newConsoleSession }: { runtime: Runtime; newConsoleSession: Props["newConsoleSession"] }) {
  const selectedTarget = { ref: runtime.connector_ref, connector_kind: runtime.connector_kind };
  const view = useConsoleConnectorView({ selectedTarget });
  const workspace = useConsoleWorkspaceSession<ConsoleRuntimeTarget>({
    selectedTarget,
    selectedRuntimeTarget: runtime,
    selectedTargetUsesLiveConsole: true,
    sessions: [],
    runtimeSelectedSession: { id: 0, status: "idle", error: null },
    attachConsoleSession: () => {},
    restartConsoleRuntime: async () => {},
    newConsoleSession,
    onOpenConnectorOperation: view.openOperation,
  });
  const Operations = view.OperationTemplate;
  return (
    <>
      <button onClick={() => void workspace.startNew(runtime)}>Start</button>
      {Operations ? (
        <Operations
          value={view.operation}
          credentials={[]}
          onChange={view.setOperation}
          onOperationComplete={async (result, operation) => {
            if (result.startConsoleSession && operation.runtimeTarget) await workspace.startNew(operation.runtimeTarget);
          }}
        />
      ) : null}
    </>
  );
}

it("uses default recovery and real registry slots to replace a fingerprint and retry its original runtime after selection changes", async () => {
  const newConsoleSession = vi.fn<Props["newConsoleSession"]>().mockRejectedValueOnce(fingerprintError).mockResolvedValueOnce(undefined);
  apiPost.mockResolvedValue({ ok: true });
  const { rerender } = render(<Workspace runtime={original} newConsoleSession={newConsoleSession} />);
  fireEvent.click(screen.getByText("Start"));
  await waitFor(() => expect(screen.getByText("SHA256:new")).toBeInTheDocument());
  expect(screen.getByText("SHA256:old")).toBeInTheDocument();
  rerender(<Workspace runtime={other} newConsoleSession={newConsoleSession} />);
  fireEvent.click(screen.getByRole("button", { name: "Replace trusted fingerprint" }));
  await waitFor(() => expect(newConsoleSession).toHaveBeenCalledTimes(2));
  expect(newConsoleSession.mock.calls[1][0]).toBe(original);
  expect(newConsoleSession.mock.calls[1][0].id).toBe(19);
  expect(apiPost).toHaveBeenCalledWith("/api/connectors/ssh/host-keys/approve", expect.objectContaining({ replace: true }), {
    signal: expect.any(AbortSignal),
  });
  await waitFor(() => expect(screen.queryByText("SHA256:new")).not.toBeInTheDocument());
});
