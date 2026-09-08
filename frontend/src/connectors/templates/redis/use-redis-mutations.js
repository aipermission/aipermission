import { useEffect, useState } from "react";
import { validateStringWrite } from "./model";
import { uniqueRedisKeys } from "./browser-helpers";

export const emptyRedisConfirmDialog = Object.freeze({
  open: false,
  type: "",
  title: "",
  description: "",
  details: [],
  tone: "warn",
  pending: false,
  error: "",
  onConfirm: null,
});

export function useRedisMutations(options) {
  const {
    resetKey,
    product,
    activeKey,
    keyResult,
    valueDraft,
    newKey,
    newValue,
    ttlDraft,
    selectedKeys,
    setState,
    setNewKey,
    setNewValue,
    setKeys,
    setSelectedKeys,
    setActiveKey,
    setKeyResult,
    setValueDraft,
    runAction,
    loadKey,
  } = options;
  const [confirmDialog, setConfirmDialog] = useState(emptyRedisConfirmDialog);

  useEffect(() => setConfirmDialog(emptyRedisConfirmDialog), [resetKey]);

  function saveStringValue(event) {
    event?.preventDefault?.();
    const key = activeKey || newKey.trim();
    const value = activeKey ? valueDraft : newValue;
    const validationError = validateStringWrite({ key, value });
    if (validationError) {
      setState({ state: "idle", error: validationError, message: "" });
      return;
    }
    const ttlSeconds = Number(ttlDraft) > 0 ? Number(ttlDraft) : 0;
    openConfirm({
      type: "save-string",
      title: activeKey ? `Save ${product} string` : `Create ${product} string key`,
      description: activeKey ? `This will overwrite the selected key as a ${product} string.` : `This will create a ${product} string key.`,
      tone: "warn",
      details: [
        { label: "Key", value: key },
        { label: "TTL", value: ttlSeconds > 0 ? `${ttlSeconds}s` : "persistent" },
      ],
      onConfirm: async () => {
        const written = await runAction({
          actionName: "set_string",
          input: { key, value, ttl_seconds: ttlSeconds },
          reason: `manual ${product} browser string write`,
          busy: "writing",
        });
        if (!written) return false;
        setNewKey("");
        setNewValue("");
        setKeys((current) => uniqueRedisKeys([...current, key]).sort());
        await loadKey(key);
        return true;
      },
    });
  }

  function updateTTL() {
    if (!activeKey) return;
    const ttlSeconds = ttlDraft.trim() === "" ? -1 : Number(ttlDraft);
    const normalizedTTL = Number.isFinite(ttlSeconds) ? ttlSeconds : -1;
    openConfirm({
      type: "ttl",
      title: normalizedTTL < 0 ? `Persist ${product} key` : `Update ${product} TTL`,
      description: normalizedTTL < 0 ? "This removes the expiration from the selected key." : "This changes when the selected key expires.",
      tone: "warn",
      details: [
        { label: "Key", value: activeKey },
        { label: "TTL", value: normalizedTTL < 0 ? "persistent" : `${normalizedTTL}s` },
      ],
      onConfirm: async () => {
        const updated = await runAction({
          actionName: "expire_key",
          input: { key: activeKey, ttl_seconds: normalizedTTL },
          reason: `manual ${product} browser TTL update`,
          busy: "writing",
        });
        if (!updated) return false;
        await loadKey(activeKey);
        return true;
      },
    });
  }

  function deleteSelected() {
    const keys = selectedKeys.length > 0 ? selectedKeys : activeKey ? [activeKey] : [];
    if (keys.length === 0) return;
    openConfirm({
      type: "delete",
      title: `Delete ${keys.length} ${product} key${keys.length === 1 ? "" : "s"}`,
      description: `This permanently deletes the selected ${product} key data.`,
      tone: "bad",
      details: keys
        .slice(0, 8)
        .map((key) => ({ label: "Key", value: key }))
        .concat(keys.length > 8 ? [{ label: "More", value: `${keys.length - 8} additional key(s)` }] : []),
      onConfirm: async () => {
        const deleted = await runAction({
          actionName: "delete_keys",
          input: { keys },
          reason: `manual ${product} browser key delete`,
          busy: "deleting",
        });
        if (!deleted) return false;
        setKeys((current) => current.filter((key) => !keys.includes(key)));
        setSelectedKeys([]);
        if (keys.includes(activeKey)) {
          setActiveKey("");
          setKeyResult(null);
          setValueDraft("");
        }
        return true;
      },
    });
  }

  function openConfirm(value) {
    setConfirmDialog({ open: true, ...value, pending: false, error: "" });
  }

  async function confirmPendingAction() {
    if (!confirmDialog.onConfirm) return;
    setConfirmDialog((current) => ({ ...current, pending: true }));
    try {
      const completed = await confirmDialog.onConfirm();
      setConfirmDialog(completed === false ? (current) => ({ ...current, pending: false }) : emptyRedisConfirmDialog);
    } catch (error) {
      setConfirmDialog((current) => ({ ...current, pending: false, error: error.message || `${product} action failed.` }));
    }
  }

  return {
    confirmDialog,
    closeConfirmDialog: () => setConfirmDialog(emptyRedisConfirmDialog),
    saveStringValue,
    updateTTL,
    deleteSelected,
    confirmPendingAction,
    creatingKey: !activeKey,
    editableString: !activeKey || keyResult?.type === "string",
  };
}
