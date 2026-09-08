import { useState } from "react";
import { apiGet, apiPost, apiPut } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";
import { selectedBinding } from "./vault-binding-utils";

const emptyBindings = {
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

export function useVaultBindings({ setAction }) {
  const [bindings, setBindings] = useState(emptyBindings);
  const guard = useRequestGuard("vault-bindings");

  async function openBindings(item) {
    guard.invalidate("mutation");
    const request = guard.begin("open");
    setBindings({ ...emptyBindings, open: true, item, state: "loading", source_project_id: String(item.owner_project_id) });
    try {
      const [bindingData, inventory] = await Promise.all([
        apiGet(`/api/vault-default-bindings?vault_item_id=${item.id}`, { signal: request.signal }),
        apiGet("/api/connector-targets/inventory", { signal: request.signal }),
      ]);
      if (!request.isCurrent()) return;
      setBindings((current) => ({
        ...current,
        state: "ready",
        data: bindingData.items || [],
        targets: vaultSessionTargets(inventory.items || []),
      }));
    } catch (error) {
      if (request.isCurrent()) setBindings((current) => ({ ...current, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }

  function closeBindings() {
    guard.invalidate("open");
    guard.invalidate("mutation");
    setBindings(emptyBindings);
  }

  async function saveBinding(event) {
    event.preventDefault();
    const snapshot = bindings;
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
      setBindings((value) => ({ ...value, state: "ready", data: result.items || [], error: null }));
      setAction({ state: "ready", message: "Default session environment binding saved.", error: null });
    } catch (error) {
      if (request.isCurrent()) setBindings((value) => ({ ...value, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }

  async function deleteBinding(item) {
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
      if (request.isCurrent()) setBindings((value) => ({ ...value, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }

  return { bindings, setBindings, openBindings, closeBindings, saveBinding, deleteBinding };
}

export function vaultSessionTargets(targets) {
  return targets
    .map((target) => ({ ...target, profiles: (target.profiles || []).filter((profile) => profile.vault_session_supported) }))
    .filter((target) => target.profiles.length > 0);
}
