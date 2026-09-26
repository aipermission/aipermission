import { useRef, useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { selectedBinding } from "./vault-binding-utils";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import { errorMessage } from "../../lib/errors.ts";
import type { VaultManagedItem, VaultActionState } from "../../lib/gateway-contracts/vault-management-contract.ts";
import { vaultBindingsResponse, vaultBindingTargetsResponse, type VaultManagedBinding, type VaultBindingTarget } from "../../lib/gateway-contracts/vault-binding-contract.ts";

export type VaultBindingsState = {
  open: boolean; item: Pick<VaultManagedItem, "id" | "owner_project_id" | "name" | "project_ids"> | null;
  state: string; data: VaultManagedBinding[]; targets: VaultBindingTarget[];
  source_project_id: string; target_id: string; profile_id: string; replace_existing: boolean; error: string | null;
};

const emptyBindings: VaultBindingsState = {
  open: false,
  item: null,
  state: "idle",
  data: [],
  targets: [],
  source_project_id: "",
  target_id: "",
  profile_id: "",
  replace_existing: false,
  error: null,
};

export function useVaultBindings({ setAction }: { setAction: Dispatch<SetStateAction<VaultActionState>> }) {
  const [bindings, setBindings] = useState(emptyBindings);
  const guard = useRequestGuard("vault-bindings");
  const mutationPending = useRef(false);

  async function openBindings(item: NonNullable<VaultBindingsState["item"]>) {
    guard.invalidate("mutation");
    mutationPending.current = false;
    const request = guard.begin("open");
    setBindings({ ...emptyBindings, open: true, item, state: "loading", source_project_id: String(item.owner_project_id) });
    try {
      const [bindingData, inventory] = await Promise.all([
        apiGet(`/api/vault-default-bindings?vault_item_id=${item.id}`, { signal: request.signal }),
        apiGet("/api/connector-targets/inventory", { signal: request.signal }),
      ]);
      if (!request.isCurrent()) return;
      const verified = vaultBindingsResponse(bindingData, item.id);
      const targets = vaultSessionTargets(vaultBindingTargetsResponse(inventory));
      setBindings((current) => ({
        ...current,
        state: "ready",
        data: verified,
        targets,
      }));
    } catch (error) {
      if (request.isCurrent()) setBindings((current) => ({ ...current, state: "error", error: errorMessage(error) }));
    } finally {
      request.complete();
    }
  }

  function closeBindings() {
    guard.invalidate("open");
    guard.invalidate("mutation");
    mutationPending.current = false;
    setBindings(emptyBindings);
  }

  async function saveBinding(event: Pick<FormEvent, "preventDefault">) {
    event.preventDefault();
    const snapshot = bindings;
    if (!snapshot.item || mutationPending.current) return;
    mutationPending.current = true;
    const current = selectedBinding(snapshot);
    const request = guard.begin("mutation");
    setBindings((value) => ({ ...value, state: "saving", error: null }));
    try {
      await apiPut(
        "/api/vault-default-bindings",
        {
          vault_item_id: snapshot.item.id,
          source_project_id: Number(snapshot.source_project_id),
          target_id: Number(snapshot.target_id),
          profile_id: Number(snapshot.profile_id),
          replace_existing: snapshot.replace_existing,
          expected_binding_revision: current?.binding_revision || 0,
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      const result = await apiGet(`/api/vault-default-bindings?vault_item_id=${snapshot.item.id}`, { signal: request.signal });
      if (!request.isCurrent()) return;
      const verified = vaultBindingsResponse(result, snapshot.item.id);
      setBindings((value) => ({ ...value, state: "ready", data: verified, error: null }));
      setAction({ state: "ready", message: "Default session environment binding saved.", error: null });
    } catch (error) {
      if (request.isCurrent()) setBindings((value) => ({ ...value, state: "error", error: errorMessage(error) }));
    } finally {
      if (request.isCurrent()) mutationPending.current = false;
      request.complete();
    }
  }

  async function deleteBinding(item: VaultManagedBinding) {
    if (mutationPending.current || !bindings.open || item.vault_item_id !== bindings.item?.id) return;
    mutationPending.current = true;
    const request = guard.begin("mutation");
    setBindings((value) => ({ ...value, state: "saving", error: null }));
    try {
      await apiPost(
        `/api/vault-default-bindings/${item.id}/delete`,
        { expected_binding_revision: item.binding_revision },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      setBindings((value) => ({ ...value, state: "ready", data: value.data.filter((binding) => binding.id !== item.id), error: null }));
      setAction({ state: "ready", message: "Default session environment binding removed.", error: null });
    } catch (error) {
      if (request.isCurrent()) setBindings((value) => ({ ...value, state: "error", error: errorMessage(error) }));
    } finally {
      if (request.isCurrent()) mutationPending.current = false;
      request.complete();
    }
  }

  return { bindings, setBindings, openBindings, closeBindings, saveBinding, deleteBinding };
}

export function vaultSessionTargets<Target extends { profiles?: { vault_session_supported: boolean }[] }>(targets: Target[]): Target[] {
  return targets
    .map((target) => ({ ...target, profiles: (target.profiles || []).filter((profile) => profile.vault_session_supported) }))
    .filter((target) => target.profiles.length > 0);
}
