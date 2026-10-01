import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ComponentProps, MouseEvent } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiPost, currentWorkspaceBinding } from "../../../../lib/api";
import { RoleReconciliationForm } from "./role-reconciliation-form";
import { roleHistoryPageFixture } from "../../../../test/postgres/role-history-fixtures.test";
import { useRoleHistory } from "./use-role-history";
import type { DatabaseProfile } from "../../_shared/database-model-types";
import type { RoleDecisionMode } from "./role-reconciliation-contract";
import type { RoleHistoryEntry } from "./role-history-types";

const { enabledClicks } = vi.hoisted(() => ({ enabledClicks: new Map<string, () => void>() }));

vi.mock("../../../../lib/api", () => ({ apiPost: vi.fn(), currentWorkspaceBinding: vi.fn() }));
vi.mock("../../../../components/ui/button", async (importOriginal) => {
  const original = await importOriginal<typeof import("../../../../components/ui/button")>();
  return {
    ...original,
    Button: (props: ComponentProps<typeof original.Button>) => {
      const { onClick, disabled, children } = props;
      if (!disabled && onClick && typeof children === "string")
        enabledClicks.set(children, () => onClick({} as MouseEvent<HTMLButtonElement>));
      return <original.Button {...props} />;
    },
  };
});

const post = vi.mocked(apiPost);
const submitted = vi.fn();
const profiles: DatabaseProfile[] = [
  { id: 2, kind: "username_password", label: "Original admin", public: { username: "admin" } },
  { id: 3, kind: "username_password", label: "Other admin" },
];
let history: ReturnType<typeof useRoleHistory>;

type HarnessProps = {
  mode: RoleDecisionMode;
  currentProfiles?: DatabaseProfile[];
  entryOverride?: RoleHistoryEntry;
  disabled?: boolean;
  showForm?: boolean;
};
function Harness({ mode, currentProfiles = profiles, entryOverride, disabled = false, showForm = true }: HarnessProps) {
  history = useRoleHistory(1);
  // Keep hook ownership intact during form-only drift so it cannot mask a stale consent handler.
  const entry = entryOverride ?? history.page?.entries[0];
  return (
    <>
      {showForm && entry ? (
        <RoleReconciliationForm
          entry={entry}
          profiles={currentProfiles}
          disabled={disabled || history.busy || history.workspaceChanged}
          onConfirm={(entry, profileID, confirmedRoleName) => {
            submitted(entry, profileID, confirmedRoleName);
            return mode === "cleanup" ? history.cleanup(entry, profileID, confirmedRoleName) : history.reconcile(entry, profileID);
          }}
        />
      ) : null}
    </>
  );
}

function submitLabel(mode: RoleDecisionMode) {
  return mode === "cleanup" ? "Confirm remote cleanup" : "Confirm role presence";
}

function openButton(mode: RoleDecisionMode) {
  return screen.getByRole("button", { name: mode === "cleanup" ? /Clean up remote role/ : /Verify role presence/ });
}

function consent(mode: RoleDecisionMode) {
  const acknowledgement = screen.getByRole("checkbox");
  if (!(acknowledgement as HTMLInputElement).checked) fireEvent.click(acknowledgement);
  if (mode === "cleanup")
    fireEvent.change(screen.getByRole("textbox", { name: "Confirm remote role name" }), {
      target: { value: history.page!.entries[0]!.record.intent.role_name },
    });
  expect(screen.getByRole("button", { name: submitLabel(mode) })).toBeEnabled();
  return enabledClicks.get(submitLabel(mode))!;
}

function mutations() {
  return post.mock.calls.filter(([path]) => /role-lifecycle-(cleanup|reconcile)$/.test(path));
}

async function ready(mode: RoleDecisionMode) {
  const journal = roleHistoryPageFixture();
  journal.entries[0]!.record.status = mode === "cleanup" ? "provisioned" : "cleanup_intent";
  post.mockResolvedValue(journal);
  const view = render(<Harness mode={mode} />);
  await waitFor(() => expect(history.state).toBe("ready"));
  fireEvent.click(openButton(mode));
  return { ...view, retained: consent(mode), entry: history.page!.entries[0]! };
}

async function confirmOnce(mode: RoleDecisionMode, retained: () => void) {
  const current = consent(mode);
  act(retained);
  expect(mutations()).toHaveLength(0);
  expect(submitted).not.toHaveBeenCalled();
  const expected = history.page!.entries[0]!;
  const entry = structuredClone(expected);
  entry.record.status = mode === "cleanup" ? "cleaned" : "provisioned";
  entry.record.generation = "d".repeat(32);
  post.mockResolvedValueOnce({
    target_id: 1,
    entry,
    evidence: mode === "cleanup" ? "acknowledged_remote_cleanup" : "exact_remote_identity_present",
  });
  await act(async () => current());
  expect(mutations()).toHaveLength(1);
  expect(submitted).toHaveBeenCalledTimes(1);
  expect(mutations()[0]![0]).toBe(`/api/connector-targets/1/operations/role-lifecycle-${mode === "cleanup" ? "cleanup" : "reconcile"}`);
  expect(mutations()[0]![1]).toEqual({
    profile_id: "2",
    input: { expected, ...(mode === "cleanup" ? { confirmed_role_name: expected.record.intent.role_name } : {}) },
  });
  expect(history.notice).toBe(
    mode === "cleanup" ? "Remote role cleanup acknowledged. Local credentials were not changed." : "Exact role presence confirmed.",
  );
  expect(post).toHaveBeenCalledTimes(3);
}

beforeEach(() => {
  enabledClicks.clear();
  submitted.mockClear();
  post.mockReset();
  vi.mocked(currentWorkspaceBinding).mockReset().mockReturnValue("workspace-a");
});

describe.each(["presence", "cleanup"] as const)("%s submission consent ownership", (mode) => {
  it.each([
    "removed",
    "username",
    "kind",
    "generation",
    "roleOID",
    "targetDigest",
    "unchecked",
    "selection",
    "reopen",
    "disabled",
    "formUnmount",
  ])("rejects retained consent across %s and its restoration", async (change) => {
    const { rerender, retained, entry } = await ready(mode);
    let restore = () => rerender(<Harness mode={mode} />);
    if (["removed", "username", "kind"].includes(change)) {
      const changed =
        change === "removed"
          ? profiles.slice(1)
          : [
              { ...profiles[0]!, ...(change === "username" ? { public: { username: "different-admin" } } : { kind: "other" }) },
              profiles[1]!,
            ];
      rerender(<Harness mode={mode} currentProfiles={changed} />);
    } else if (["generation", "roleOID", "targetDigest"].includes(change)) {
      const changed = structuredClone(entry);
      if (change === "generation") changed.record.generation = "c".repeat(32);
      if (change === "roleOID") changed.record.role_oid++;
      if (change === "targetDigest") changed.record.intent.anchor.target_digest = "e".repeat(64);
      rerender(<Harness mode={mode} entryOverride={changed} />);
    } else if (change === "disabled") rerender(<Harness mode={mode} disabled />);
    else if (change === "formUnmount") {
      rerender(<Harness mode={mode} showForm={false} />);
      restore = () => {
        rerender(<Harness mode={mode} />);
        fireEvent.click(openButton(mode));
      };
    } else {
      act(() => {
        if (change === "unchecked") fireEvent.click(screen.getByRole("checkbox"));
        if (change === "selection") fireEvent.change(screen.getByRole("combobox"), { target: { value: "3" } });
        if (change === "reopen") fireEvent.click(openButton(mode));
        retained();
        expect(submitted).not.toHaveBeenCalled();
        expect(mutations()).toHaveLength(0);
      });
      if (change === "selection")
        restore = () => {
          fireEvent.change(screen.getByRole("combobox"), { target: { value: "2" } });
        };
      if (change === "reopen")
        restore = () => {
          fireEvent.click(openButton(mode));
        };
    }
    act(retained);
    expect(history.state).toBe("ready");
    expect(mutations()).toHaveLength(0);
    expect(submitted).not.toHaveBeenCalled();
    expect(post).toHaveBeenCalledTimes(1);
    restore();
    if (change !== "disabled") expect(screen.getByRole("checkbox")).not.toBeChecked();
    await confirmOnce(mode, retained);
  });

  it("rejects a retained handler after the form and history hook unmount", async () => {
    const { unmount, retained } = await ready(mode);
    unmount();
    act(retained);
    expect(mutations()).toHaveLength(0);
    expect(post).toHaveBeenCalledTimes(1);
  });

  it("admits one synchronous double invocation, blocks busy handlers and never retries an unknown outcome", async () => {
    const { retained, entry } = await ready(mode);
    let reject!: (_error: Error) => void;
    const pending = new Promise<unknown>((_resolve, no) => {
      reject = no;
    });
    post.mockReturnValueOnce(pending);
    act(() => {
      retained();
      retained();
      expect(mutations()).toHaveLength(1);
      expect(submitted).toHaveBeenCalledTimes(1);
    });
    expect(history.busy).toBe(true);
    expect(screen.getByRole("button", { name: submitLabel(mode) })).toBeDisabled();
    act(retained);
    expect(mutations()).toHaveLength(1);
    expect(submitted).toHaveBeenCalledTimes(1);
    expect(mutations()[0]![1]).toEqual({
      profile_id: "2",
      input: { expected: entry, ...(mode === "cleanup" ? { confirmed_role_name: entry.record.intent.role_name } : {}) },
    });
    await act(async () => {
      reject(new Error("Synthetic lost decision acknowledgement"));
      await pending.catch(() => {});
    });
    await waitFor(() => expect(history.busy).toBe(false));
    expect(history.state).toBe("ready");
    expect(history.notice).toMatch(/^Decision outcome was not confirmed/);
    act(retained);
    expect(mutations()).toHaveLength(1);
    expect(submitted).toHaveBeenCalledTimes(1);
    expect(post).toHaveBeenCalledTimes(3);
  });
});

it("invalidates cleanup synchronously on typed-name change and does not revive the handler when exact text returns", async () => {
  const { retained, entry } = await ready("cleanup");
  const input = screen.getByRole("textbox", { name: "Confirm remote role name" });
  act(() => {
    fireEvent.change(input, { target: { value: `${entry.record.intent.role_name} ` } });
    retained();
    expect(mutations()).toHaveLength(0);
  });
  expect(screen.getByRole("button", { name: submitLabel("cleanup") })).toBeDisabled();
  fireEvent.change(input, { target: { value: entry.record.intent.role_name } });
  act(retained);
  expect(mutations()).toHaveLength(0);
  await confirmOnce("cleanup", retained);
});
