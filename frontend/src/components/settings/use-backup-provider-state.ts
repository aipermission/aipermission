import { uploadedBackupRecordResponse } from "./backup-contracts";
import { useEffect, useState, type FormEvent } from "react";
import { apiDelete, apiDownload, apiGet, apiPost, apiPut } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useAsyncAction } from "../../lib/use-async-action";
import { useBackupRecordState } from "./use-backup-record-state";
import {
  backupItems,
  isBackupCatalogItem,
  isBackupProvider,
  type BackupCatalogItem,
  type BackupProvider,
  type LoadState,
} from "./backup-contracts";

export { suggestedRestoreDatabaseName } from "./use-backup-record-state";

const emptyState = { state: "idle", error: null, message: null };

type DatabaseState = { data?: { database_name?: string } | null };
type BackupProviderForm = { provider_type: string; name: string; base_url: string; token: string };
type ProviderPayload = { provider_type: string; name: string; public: { base_url: string }; secret?: { token: string } };

export function useBackupProviderState(database: DatabaseState) {
  const { actionState: backupState, runAction: runBackupAction } = useAsyncAction(emptyState);
  const {
    actionState: backupProviderState,
    runAction: runBackupProviderAction,
    resetAction: resetBackupProviderAction,
  } = useAsyncAction(emptyState);
  const [backupProviderCatalog, setBackupProviderCatalog] = useState<LoadState<BackupCatalogItem>>({
    state: "loading",
    data: [],
    error: null,
  });
  const [backupProviders, setBackupProviders] = useState<LoadState<BackupProvider>>({ state: "loading", data: [], error: null });
  const [backupProviderDialogOpen, setBackupProviderDialogOpen] = useState(false);
  const [backupProviderArchiveTarget, setBackupProviderArchiveTarget] = useState<BackupProvider | null>(null);
  const [backupProviderEditingID, setBackupProviderEditingID] = useState<number | null>(null);
  const [backupEnableTarget, setBackupEnableTarget] = useState<BackupProvider | null>(null);
  const [backupEnablePassword, setBackupEnablePassword] = useState("");
  const [backupUploadTarget, setBackupUploadTarget] = useState<BackupProvider | null>(null);
  const [backupProviderForm, setBackupProviderForm] = useState<BackupProviderForm>(emptyBackupProviderForm);

  async function loadBackupProviderCatalog() {
    try {
      const data = await apiGet("/api/backup/providers/catalog");
      setBackupProviderCatalog({ state: "ready", data: backupItems(data, isBackupCatalogItem), error: null });
    } catch (error) {
      setBackupProviderCatalog({ state: "error", data: [], error: errorMessage(error, "Unable to load backup catalog.") });
    }
  }

  async function loadBackupProviders() {
    try {
      const data = await apiGet("/api/backup/providers");
      setBackupProviders({ state: "ready", data: backupItems(data, isBackupProvider), error: null });
    } catch (error) {
      setBackupProviders({ state: "error", data: [], error: errorMessage(error, "Unable to load backup providers.") });
    }
  }

  useEffect(() => {
    void loadBackupProviderCatalog();
    void loadBackupProviders();
  }, []);

  const databaseName = database.data?.database_name || "Unknown";
  const backupRecordState = useBackupRecordState({ backupProviderState, runBackupProviderAction, resetBackupProviderAction });

  async function downloadDatabase() {
    await runBackupAction({
      pending: "downloading",
      successMessage: (result) => (result?.canceled ? null : "Encrypted database downloaded."),
      action: () =>
        apiDownload("/api/backup/download", `${databaseName}-${new Date().toISOString().slice(0, 19)}.aipdb`, {
          picker: true,
          requireStreaming: true,
        }),
    });
  }

  function openBackupProviderDialog(provider: BackupProvider | null = null) {
    resetBackupProviderAction();
    if (provider) {
      setBackupProviderEditingID(provider.id);
      setBackupProviderForm({
        provider_type: provider.provider_type || "aipermission_backup",
        name: provider.name,
        base_url: provider.public?.base_url || "",
        token: "",
      });
    } else {
      const firstType = backupProviderCatalog.data[0]?.provider_type || "aipermission_backup";
      setBackupProviderEditingID(null);
      setBackupProviderForm({
        provider_type: firstType,
        name: backupProviderLabel(firstType, backupProviderCatalog.data),
        base_url: "",
        token: "",
      });
    }
    setBackupProviderDialogOpen(true);
  }

  function closeBackupProviderDialog() {
    if (backupProviderState.state === "saving") return;
    setBackupProviderDialogOpen(false);
    setBackupProviderEditingID(null);
    setBackupProviderForm(emptyBackupProviderForm());
  }

  function updateBackupProviderField(field: keyof BackupProviderForm, value: string) {
    setBackupProviderForm((current) => ({ ...current, [field]: value }));
  }

  async function saveBackupProvider(event: Pick<FormEvent, "preventDefault">) {
    event.preventDefault();
    const payload: ProviderPayload = {
      provider_type: backupProviderForm.provider_type,
      name: backupProviderForm.name,
      public: {
        base_url: backupProviderForm.base_url.trim(),
      },
    };
    if (backupProviderForm.token.trim()) {
      payload.secret = { token: backupProviderForm.token.trim() };
    }
    await runBackupProviderAction({
      pending: "saving",
      successMessage: backupProviderEditingID ? "Backup provider updated." : "Backup provider added.",
      action: async () => {
        if (backupProviderEditingID) {
          await apiPut(`/api/backup/providers/${backupProviderEditingID}`, payload);
        } else {
          await apiPost("/api/backup/providers", payload);
        }
        setBackupProviderDialogOpen(false);
        setBackupProviderEditingID(null);
        setBackupProviderForm(emptyBackupProviderForm());
        await loadBackupProviders();
      },
    });
  }

  async function testBackupProvider(provider: BackupProvider) {
    await runBackupProviderAction({
      pending: `testing-${provider.id}`,
      successMessage: `${provider.name} is reachable and protocol-compatible.`,
      action: () => apiPost(`/api/backup/providers/${provider.id}/test`, {}),
    });
    await loadBackupProviders();
  }

  async function disableBackupProvider(provider: BackupProvider) {
    await runBackupProviderAction({
      pending: `disabling-${provider.id}`,
      successMessage: `${provider.name} disabled.`,
      action: () => apiPut(`/api/backup/providers/${provider.id}`, { name: provider.name, status: "disabled" }),
    });
    await loadBackupProviders();
  }

  function requestEnableBackupProvider(provider: BackupProvider) {
    resetBackupProviderAction();
    setBackupEnableTarget(provider);
    setBackupEnablePassword("");
  }

  function closeEnableBackupProviderDialog() {
    if (backupProviderState.state === `enabling-${backupEnableTarget?.id}`) return;
    setBackupEnableTarget(null);
    setBackupEnablePassword("");
  }

  async function enableBackupProvider(event: FormEvent) {
    event.preventDefault();
    const provider = backupEnableTarget;
    if (!provider) return;
    const result = await runBackupProviderAction({
      pending: `enabling-${provider.id}`,
      successMessage: `${provider.name} enabled.`,
      action: () => apiPost(`/api/backup/providers/${provider.id}/enable`, { current_password: backupEnablePassword }),
    });
    if (result !== undefined) {
      closeEnableBackupProviderDialog();
      await loadBackupProviders();
    }
  }

  function closeBackupProviderArchiveDialog() {
    if (backupProviderState.state === "archiving") return;
    setBackupProviderArchiveTarget(null);
  }

  function requestArchiveBackupProvider(provider: BackupProvider) {
    resetBackupProviderAction();
    setBackupProviderArchiveTarget(provider);
  }

  async function archiveBackupProvider(event: FormEvent) {
    event.preventDefault();
    const provider = backupProviderArchiveTarget;
    if (!provider) return;
    await runBackupProviderAction({
      pending: "archiving",
      successMessage: `Archived backup provider "${provider.name}".`,
      action: async () => {
        await apiDelete(`/api/backup/providers/${provider.id}`);
        setBackupProviderArchiveTarget(null);
        await loadBackupProviders();
      },
    });
  }

  function requestUploadBackupProvider(provider: BackupProvider) {
    resetBackupProviderAction();
    setBackupUploadTarget(provider);
  }

  function closeUploadBackupDialog() {
    if (backupProviderState.state === `uploading-${backupUploadTarget?.id}`) return;
    setBackupUploadTarget(null);
  }

  async function uploadBackupProvider(event: FormEvent) {
    event.preventDefault();
    const provider = backupUploadTarget;
    if (!provider) return;
    await runBackupProviderAction({
      pending: `uploading-${provider.id}`,
      successMessage: (record: { filename: string }) => `Uploaded ${record.filename} to ${provider.name}.`,
      action: async () => {
        const record = uploadedBackupRecordResponse(await apiPost(`/api/backup/providers/${provider.id}/upload`, {}));
        setBackupUploadTarget(null);
        await loadBackupProviders();
        return record;
      },
    });
  }

  return {
    backupState,
    backupProviderState,
    backupProviderCatalog,
    backupProviders,
    backupProviderDialogOpen,
    backupProviderArchiveTarget,
    backupProviderEditingID,
    backupEnableTarget,
    backupEnablePassword,
    setBackupEnablePassword,
    backupUploadTarget,
    backupProviderForm,
    setBackupProviderForm,
    databaseName,
    ...backupRecordState,
    downloadDatabase,
    openBackupProviderDialog,
    closeBackupProviderDialog,
    updateBackupProviderField,
    saveBackupProvider,
    testBackupProvider,
    disableBackupProvider,
    requestEnableBackupProvider,
    closeEnableBackupProviderDialog,
    enableBackupProvider,
    requestArchiveBackupProvider,
    closeBackupProviderArchiveDialog,
    archiveBackupProvider,
    requestUploadBackupProvider,
    closeUploadBackupDialog,
    uploadBackupProvider,
  };
}

export function backupProviderLabel(providerType: string, catalog: BackupCatalogItem[]) {
  return catalog.find((item) => item.provider_type === providerType)?.label || providerType;
}

function emptyBackupProviderForm() {
  return {
    provider_type: "aipermission_backup",
    name: "AIPermission Backup",
    base_url: "",
    token: "",
  };
}
