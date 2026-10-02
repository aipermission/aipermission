import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { apiGet, apiPut } from "../../lib/api";
import { capabilitySnapshot, deferred, scopeSnapshot } from "../../test/tokens/vault-permission-support";
import { VaultPermissionDialog } from "./vault-permission-dialog";

vi.mock("../../lib/api", () => ({ apiGet: vi.fn(), apiPut: vi.fn() }));

const token = { id: 7, name: "agent" };
const savedMessage = "Project Vault capabilities saved.";
const newerMessage = "Submitted Vault capabilities saved. Newer edits are not saved.";

function saveButton() {
  return screen.getByRole("button", { name: "Save Vault capabilities" });
}

function expectRule(name: string) {
  expect(screen.getByRole("button", { name })).toHaveClass("permission-button-active");
}

beforeEach(() => {
  vi.mocked(apiGet)
    .mockReset()
    .mockImplementation(async (path) => (path.endsWith("/project-scopes") ? scopeSnapshot() : capabilitySnapshot()));
  vi.mocked(apiPut).mockReset().mockResolvedValue(capabilitySnapshot("capability-3", "approval_required"));
});

describe("Vault permission draft ownership", () => {
  it.each(["rule", "lifetime", "disabled", "ABA"])(
    "preserves a newer %s edit and uses the committed revision on the next PUT",
    async (edit) => {
      const user = userEvent.setup();
      const pending = deferred();
      const onSaved = vi.fn();
      vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
      render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
      await screen.findByText("Inject secrets");
      await user.click(screen.getByRole("button", { name: "Always" }));
      await user.click(saveButton());
      const submitted = vi.mocked(apiPut).mock.calls[0][1];
      let rule = "always_run";
      let expiresAt: unknown = undefined;
      if (edit === "rule") {
        await user.click(screen.getByRole("button", { name: "Prompt" }));
        rule = "approval_required";
      } else if (edit === "lifetime") {
        vi.spyOn(Date, "now").mockReturnValue(Date.parse("2030-01-01T00:00:00Z"));
        await user.click(screen.getByRole("button", { name: "1h" }));
        expiresAt = "2030-01-01T01:00:00.000Z";
      } else if (edit === "disabled") {
        await user.click(screen.getByRole("button", { name: "Disabled" }));
        rule = "";
      } else {
        await user.click(screen.getByRole("button", { name: "Prompt" }));
        await user.click(screen.getByRole("button", { name: "Always" }));
      }
      await act(async () => pending.resolve(capabilitySnapshot("capability-2", "always_run")));
      expectRule(rule === "always_run" ? "Always" : rule ? "Prompt" : "Disabled");
      expect(screen.queryByText(savedMessage)).not.toBeInTheDocument();
      expect(screen.getByText(newerMessage)).toBeVisible();
      expect(onSaved).toHaveBeenCalledOnce();
      await user.click(saveButton());
      expect(apiPut).toHaveBeenLastCalledWith(
        "/api/tokens/7/project-capabilities",
        {
          capabilities: rule ? [{ project_id: 3, capability_name: "vault.inject", execution_rule: rule, expires_at: expiresAt }] : [],
          expected_revision: "capability-2",
        },
        { signal: expect.any(AbortSignal) },
      );
      expect(submitted).toEqual({
        capabilities: [{ project_id: 3, capability_name: "vault.inject", execution_rule: "always_run", expires_at: undefined }],
        expected_revision: "capability-1",
      });
    },
  );

  it("adopts server normalization only for an unchanged draft and clears success when edited", async () => {
    const user = userEvent.setup();
    const pending = deferred();
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(saveButton());
    await act(async () => pending.resolve(capabilitySnapshot("capability-2", "approval_required")));
    expectRule("Prompt");
    expect(screen.getByText(savedMessage)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "1h" }));
    expect(screen.queryByText(savedMessage)).not.toBeInTheDocument();
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-2" });
  });

  it.each(["transport", "conflict", "malformed"])("keeps the newer draft and unadvanced revision after a %s failure", async (failure) => {
    const user = userEvent.setup();
    const pending = deferred();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(saveButton());
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    await act(async () => {
      if (failure === "malformed") pending.resolve({ ...capabilitySnapshot(), revision: "" });
      else pending.reject(new Error(failure === "conflict" ? "409: Reload permissions" : "Save unavailable"));
    });
    expectRule("Prompt");
    expect(
      screen.getByText(
        failure === "malformed"
          ? "Invalid Vault capability response from gateway."
          : failure === "conflict"
            ? "409: Reload permissions"
            : "Save unavailable",
      ),
    ).toBeVisible();
    expect(screen.queryByText(savedMessage)).not.toBeInTheDocument();
    expect(screen.queryByText(newerMessage)).not.toBeInTheDocument();
    expect(onSaved).not.toHaveBeenCalled();
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({
      expected_revision: "capability-1",
      capabilities: [{ execution_rule: "approval_required" }],
    });
    expect(screen.queryByText("Save unavailable")).not.toBeInTheDocument();
  });

  it("admits only one synchronous submission and retains admission on a same-identity rerender", async () => {
    const pending = deferred();
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    const view = render(<VaultPermissionDialog token={token} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    const form = saveButton().closest("form")!;
    act(() => {
      fireEvent.submit(form);
      fireEvent.submit(form);
    });
    expect(apiPut).toHaveBeenCalledOnce();
    const signal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
    view.rerender(<VaultPermissionDialog token={{ ...token }} onClose={vi.fn()} />);
    fireEvent.submit(form);
    expect(apiPut).toHaveBeenCalledOnce();
    expect(signal?.aborted).toBe(false);
    expect(apiGet).toHaveBeenCalledTimes(2);
    await act(async () => pending.resolve(capabilitySnapshot("capability-2")));
    fireEvent.submit(form);
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-2" });
  });

  it.each(["scope-first", "capability-first"])("keeps scope and capability revisions independent in %s response order", async (order) => {
    const user = userEvent.setup();
    const scope = deferred();
    const capability = deferred();
    vi.mocked(apiPut).mockImplementation((path) => (path.endsWith("/project-scopes") ? scope.promise : capability.promise));
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(saveButton());
    await user.click(screen.getByRole("checkbox"));
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    const finishScope = () => scope.resolve(scopeSnapshot("scope-2", false));
    const finishCapability = () => capability.resolve(capabilitySnapshot("capability-2", "always_run"));
    await act(async () => (order === "scope-first" ? finishScope() : finishCapability()));
    await act(async () => (order === "scope-first" ? finishCapability() : finishScope()));
    expectRule("Prompt");
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    vi.mocked(apiPut).mockResolvedValue(capabilitySnapshot("capability-3", "approval_required"));
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[2][1]).toMatchObject({
      expected_revision: "capability-2",
      capabilities: [{ execution_rule: "approval_required" }],
    });
    vi.mocked(apiPut).mockResolvedValue(scopeSnapshot("scope-3"));
    await user.click(screen.getByRole("checkbox"));
    expect(vi.mocked(apiPut).mock.calls[3][1]).toEqual({ enabled_project_ids: [3], expected_revision: "scope-2" });
  });
});

describe("Vault permission lifecycle ownership", () => {
  it.each(["close/reopen", "ABA"])("%s retires an old save without blocking the next same-token save", async (transition) => {
    const user = userEvent.setup();
    const old = deferred();
    const current = deferred();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
    const view = render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    await user.click(saveButton());
    const oldSignal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
    view.rerender(
      <VaultPermissionDialog token={transition === "ABA" ? { id: 8, name: "other" } : null} onClose={vi.fn()} onSaved={onSaved} />,
    );
    vi.mocked(apiGet).mockImplementation(async (path) =>
      path.endsWith("/project-scopes") ? scopeSnapshot("scope-reopen") : capabilitySnapshot("capability-reopen"),
    );
    view.rerender(<VaultPermissionDialog token={{ ...token }} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    expect(saveButton()).toBeEnabled();
    expectRule("Disabled");
    expect(oldSignal?.aborted).toBe(true);
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-reopen" });
    await act(async () => old.resolve(capabilitySnapshot("obsolete-revision", "always_run")));
    expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled();
    expectRule("Prompt");
    expect(onSaved).not.toHaveBeenCalled();
    await act(async () => current.resolve(capabilitySnapshot("capability-current", "approval_required")));
    expect(onSaved).toHaveBeenCalledOnce();
  });

  it.each([
    ["old-first", "resolve"],
    ["new-first", "resolve"],
    ["old-first", "reject"],
    ["new-first", "reject"],
  ])("ignores retired same-token completions in %s order with an old %s", async (order, settlement) => {
    const user = userEvent.setup();
    const old = deferred();
    const current = deferred();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(old.promise).mockReturnValueOnce(current.promise);
    const view = render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(saveButton());
    view.rerender(<VaultPermissionDialog token={null} onClose={vi.fn()} onSaved={onSaved} />);
    view.rerender(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    await user.click(saveButton());
    const finishOld = () =>
      settlement === "resolve"
        ? old.resolve(capabilitySnapshot("obsolete-revision", "always_run"))
        : old.reject(new Error("Retired save failed"));
    const finishCurrent = () => current.resolve(capabilitySnapshot("newest-revision", "approval_required"));
    await act(async () => (order === "old-first" ? finishOld() : finishCurrent()));
    await act(async () => (order === "old-first" ? finishCurrent() : finishOld()));
    expectRule("Prompt");
    expect(screen.queryByText("Retired save failed")).not.toBeInTheDocument();
    expect(onSaved).toHaveBeenCalledOnce();
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[2][1]).toMatchObject({ expected_revision: "newest-revision" });
  });

  it.each(["resolve", "reject"])("unmount aborts a save and suppresses its late %s callback", async (settlement) => {
    const user = userEvent.setup();
    const pending = deferred();
    const onSaved = vi.fn();
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    const view = render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(saveButton());
    const signal = vi.mocked(apiPut).mock.calls[0][2]?.signal;
    view.unmount();
    expect(signal?.aborted).toBe(true);
    await act(async () =>
      settlement === "resolve" ? pending.resolve(capabilitySnapshot("late")) : pending.reject(new Error("Late failure")),
    );
    expect(onSaved).not.toHaveBeenCalled();
  });

  it("reports refresh failure separately from a committed save and allows the next revision", async () => {
    const user = userEvent.setup();
    const onSaved = vi.fn().mockRejectedValue(new Error("Refresh unavailable"));
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(saveButton());
    expect(await screen.findByText("Vault capabilities saved, but refreshing token data failed: Refresh unavailable")).toBeVisible();
    expect(screen.getByText(savedMessage)).toBeVisible();
    await user.click(saveButton());
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-3" });
  });

  it("releases admission after persistence and does not let an old refresh failure affect a newer save", async () => {
    const user = userEvent.setup();
    const refresh = deferred();
    const secondSave = deferred();
    const onSaved = vi.fn().mockReturnValueOnce(refresh.promise).mockResolvedValue(undefined);
    vi.mocked(apiPut).mockResolvedValueOnce(capabilitySnapshot("capability-2", "always_run")).mockReturnValueOnce(secondSave.promise);
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(saveButton());
    expect(screen.getByText(savedMessage)).toBeVisible();
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    await user.click(saveButton());
    expect(apiPut).toHaveBeenCalledTimes(2);
    expect(vi.mocked(apiPut).mock.calls[1][1]).toMatchObject({ expected_revision: "capability-2" });
    await act(async () => refresh.reject(new Error("Old refresh failure")));
    expect(screen.queryByText(/Old refresh failure/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Saving..." })).toBeDisabled();
    await act(async () => secondSave.resolve(capabilitySnapshot("capability-3", "approval_required")));
    expectRule("Prompt");
    expect(onSaved).toHaveBeenCalledTimes(2);
  });

  it("keeps the same-token draft across parent refresh rerenders", async () => {
    const user = userEvent.setup();
    const pending = deferred();
    const view = render(<VaultPermissionDialog token={token} onClose={vi.fn()} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("button", { name: "Always" }));
    vi.mocked(apiPut).mockReturnValueOnce(pending.promise);
    await user.click(saveButton());
    await user.click(screen.getByRole("button", { name: "Prompt" }));
    view.rerender(<VaultPermissionDialog token={{ ...token, name: "renamed" }} onClose={vi.fn()} onSaved={vi.fn()} />);
    expectRule("Prompt");
    expect(apiGet).toHaveBeenCalledTimes(2);
    await act(async () => pending.resolve(capabilitySnapshot("capability-2", "always_run")));
    expectRule("Prompt");
    expect(screen.getByText(newerMessage)).toBeVisible();
  });

  it("does not admit form submissions while a load is pending or failed", async () => {
    const pending = deferred();
    vi.mocked(apiGet).mockReturnValue(pending.promise);
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} />);
    fireEvent.submit(saveButton().closest("form")!);
    expect(apiPut).not.toHaveBeenCalled();
    await act(async () => pending.reject(new Error("Load failed")));
    fireEvent.submit(saveButton().closest("form")!);
    expect(apiPut).not.toHaveBeenCalled();
  });

  it("does not roll back committed visibility when its refresh callback fails", async () => {
    const user = userEvent.setup();
    vi.mocked(apiPut).mockResolvedValue(scopeSnapshot("scope-2", false));
    const onSaved = vi.fn().mockRejectedValue(new Error("Refresh unavailable"));
    render(<VaultPermissionDialog token={token} onClose={vi.fn()} onSaved={onSaved} />);
    await screen.findByText("Inject secrets");
    await user.click(screen.getByRole("checkbox"));
    expect(await screen.findByText("Project visibility saved, but refreshing token data failed: Refresh unavailable")).toBeVisible();
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    vi.mocked(apiPut).mockResolvedValue(scopeSnapshot("scope-3"));
    await user.click(screen.getByRole("checkbox"));
    expect(vi.mocked(apiPut).mock.calls[1][1]).toEqual({ enabled_project_ids: [3], expected_revision: "scope-2" });
  });
});
