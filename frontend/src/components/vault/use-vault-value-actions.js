import { useCallback, useEffect, useState } from "react";
import { apiPost } from "../../lib/api";
import { useRequestGuard } from "../../lib/request-guard";

export const emptyVaultReplace = {
  open: false,
  item: null,
  source: "imported",
  value: "",
  generator_kind: "random_token",
  preview_value: "",
  preview_token: "",
  preview_state: "idle",
  state: "idle",
  error: null,
};

const emptyReveal = { open: false, item: null, state: "idle", value: "", error: null, copied: false };
const emptyRemove = { open: false, item: null, confirm: "", state: "idle", error: null };

export function useVaultValueActions({ reloadItems, setAction }) {
  const [reveal, setReveal] = useState(emptyReveal);
  const [replace, setReplace] = useState(emptyVaultReplace);
  const [remove, setRemove] = useState(emptyRemove);
  const guard = useRequestGuard("vault-values");
  const closeReveal = useCallback(() => {
    guard.invalidate("reveal");
    guard.invalidate("clipboard");
    setReveal(emptyReveal);
  }, [guard]);

  useEffect(() => {
    if (!reveal.open || !reveal.value) return undefined;
    const timer = window.setTimeout(closeReveal, 30000);
    return () => window.clearTimeout(timer);
  }, [reveal.open, reveal.value, closeReveal]);

  useEffect(() => {
    if (!replace.open || !replace.preview_value) return undefined;
    const timer = window.setTimeout(() => {
      guard.invalidate("replace-preview");
      setReplace((current) => ({ ...current, preview_value: "", preview_token: "", preview_state: "idle" }));
    }, 30000);
    return () => window.clearTimeout(timer);
  }, [replace.open, replace.preview_value, guard]);

  async function openReveal(item) {
    guard.invalidate("clipboard");
    const request = guard.begin("reveal");
    setReveal({ open: true, item, state: "loading", value: "", error: null, copied: false });
    try {
      const data = await apiPost(`/api/vault-items/${item.id}/reveal`, {}, { signal: request.signal });
      if (request.isCurrent()) setReveal({ open: true, item, state: "ready", value: data.value || "", error: null, copied: false });
    } catch (error) {
      if (request.isCurrent()) setReveal({ open: true, item, state: "error", value: "", error: error.message, copied: false });
    } finally {
      request.complete();
    }
  }

  async function copyRevealedValue() {
    if (!reveal.value) return;
    const request = guard.begin("clipboard");
    try {
      await navigator.clipboard.writeText(reveal.value);
      if (request.isCurrent()) setReveal((current) => ({ ...current, copied: true }));
    } catch {
      if (request.isCurrent()) setReveal((current) => ({ ...current, error: "Clipboard access failed." }));
    } finally {
      request.complete();
    }
  }

  function openReplace(item) {
    guard.invalidate("replace-value");
    setReplace({ ...emptyVaultReplace, open: true, item });
  }

  function closeReplace() {
    guard.invalidate("replace-preview");
    guard.invalidate("replace-value");
    setReplace(emptyVaultReplace);
  }

  function selectImportedReplacement() {
    guard.invalidate("replace-preview");
    setReplace((current) => ({
      ...current,
      source: "imported",
      preview_value: "",
      preview_token: "",
      preview_state: "idle",
      error: null,
    }));
  }

  async function generateReplacementPreview(item, generatorKind) {
    if (!item || !generatorKind) return;
    const request = guard.begin("replace-preview");
    setReplace((current) => ({
      ...current,
      source: "generated",
      generator_kind: generatorKind,
      value: "",
      preview_value: "",
      preview_token: "",
      preview_state: "loading",
      error: null,
    }));
    try {
      const data = await apiPost(
        `/api/vault-items/${item.id}/generate-preview`,
        { generator_kind: generatorKind },
        { signal: request.signal },
      );
      if (request.isCurrent()) {
        setReplace((current) => ({
          ...current,
          preview_value: data.value || "",
          preview_token: data.preview_token || "",
          preview_state: "ready",
          error: null,
        }));
      }
    } catch (error) {
      if (request.isCurrent()) {
        setReplace((current) => ({ ...current, preview_value: "", preview_token: "", preview_state: "error", error: error.message }));
      }
    } finally {
      request.complete();
    }
  }

  async function replaceValue(event) {
    event.preventDefault();
    if (!replace.item) return;
    const snapshot = replace;
    const request = guard.begin("replace-value");
    setReplace((current) => ({ ...current, state: "saving", error: null }));
    try {
      await apiPost(
        `/api/vault-items/${snapshot.item.id}/value`,
        {
          source: snapshot.source,
          value: snapshot.source === "imported" ? snapshot.value : "",
          generator_kind: snapshot.source === "generated" ? snapshot.generator_kind : "",
          preview_token: snapshot.source === "generated" ? snapshot.preview_token : "",
          expected_value_version: snapshot.item.value_version,
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      setReplace(emptyVaultReplace);
      setAction({
        state: "ready",
        message:
          snapshot.source === "generated"
            ? "A new local Vault value was generated. Provider-side credentials were not rotated."
            : "Local Vault value replaced. Provider-side credentials were not rotated.",
        error: null,
      });
      await reloadItems();
    } catch (error) {
      if (request.isCurrent()) setReplace((current) => ({ ...current, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }

  function openRemove(item) {
    guard.invalidate("remove");
    setRemove({ ...emptyRemove, open: true, item });
  }

  function closeRemove() {
    guard.invalidate("remove");
    setRemove(emptyRemove);
  }

  async function deleteItem() {
    if (!remove.item) return;
    const item = remove.item;
    const request = guard.begin("remove");
    setRemove((current) => ({ ...current, state: "deleting", error: null }));
    try {
      await apiPost(
        `/api/vault-items/${item.id}/delete`,
        {
          expected_value_version: item.value_version,
          expected_metadata_revision: item.metadata_revision,
        },
        { signal: request.signal },
      );
      if (!request.isCurrent()) return;
      setRemove(emptyRemove);
      setAction({ state: "ready", message: "Vault item deleted from the active database.", error: null });
      await reloadItems();
    } catch (error) {
      if (request.isCurrent()) setRemove((current) => ({ ...current, state: "error", error: error.message }));
    } finally {
      request.complete();
    }
  }

  return {
    reveal,
    replace,
    setReplace,
    remove,
    setRemove,
    openReveal,
    closeReveal,
    copyRevealedValue,
    openReplace,
    closeReplace,
    selectImportedReplacement,
    generateReplacementPreview,
    replaceValue,
    openRemove,
    closeRemove,
    deleteItem,
  };
}
