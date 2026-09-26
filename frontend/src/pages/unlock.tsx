import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import type { DatabaseCatalogItem, DatabaseStatus } from "../lib/gateway-contracts/database-status-contract.ts";
import { Notice } from "../components/ui/notice";
import { appVersion } from "../lib/release";
import { RemoteRestorePanel } from "./remote-restore-panel";
import { UnlockCreatePanel } from "./unlock-create-panel";
import { UnlockDatabasePanel } from "./unlock-database-panel";
import { UnlockImportPanel } from "./unlock-import-panel";
import { useUnlockLifecycleMutation } from "./use-unlock-lifecycle-mutation";

type Database = Pick<DatabaseCatalogItem, "id" | "name" | "state">;
type Tab = "unlock" | "create" | "import" | "remote";
type LifecycleMutation = ReturnType<typeof useUnlockLifecycleMutation>["runMutation"];
type Props = {
  status: (Pick<DatabaseStatus, "state" | "database_id"> & { databases: Database[] }) | null;
  onUnlocked: (_signal: AbortSignal) => void | Promise<unknown>;
};
type ActivePanelProps = {
  activeTab: Tab;
  selectedDatabase: Database | null;
  unsupported: boolean;
  migrationRequired: boolean;
  hasDatabase: boolean;
  onMigrationRequired: (_id: string) => void;
  onDeleted: (_id: string) => void;
  runLifecycleMutation: LifecycleMutation;
};

function unlockTabsGridClass(tabCount: number) {
  return tabCount === 4 ? "grid-cols-2 sm:grid-cols-4" : "grid-cols-2 sm:grid-cols-3";
}

export function UnlockPage({ status, onUnlocked }: Props) {
  const databases = useMemo(() => status?.databases || [], [status?.databases]);
  const firstDatabaseID = status?.database_id || databases[0]?.id || "default";
  const [selectedDatabaseID, setSelectedDatabaseID] = useState(firstDatabaseID);
  const selectedDatabase = databases.find((database) => database.id === selectedDatabaseID) || databases[0] || null;
  const hasDatabase = Boolean(selectedDatabase);
  const selectedUnsupported = selectedDatabase?.state === "unsupported_plaintext";
  const [migrationRequiredIDs, setMigrationRequiredIDs] = useState<Record<string, boolean>>({});
  const selectedMigrationRequired = Boolean(selectedDatabase && migrationRequiredIDs[selectedDatabase.id]);
  const [activeTab, setActiveTab] = useState<Tab>(hasDatabase ? "unlock" : "create");
  const [toast, setToast] = useState("");
  const toastTimerRef = useRef<number | null>(null);
  const lifecycleMutation = useUnlockLifecycleMutation(onUnlocked);
  const statusSelectionKey = `${status?.database_id || ""}:${databases.map((database) => `${database.id}:${database.state || ""}`).join("|")}`;
  const appliedStatusSelectionRef = useRef(statusSelectionKey);

  useEffect(() => {
    if (lifecycleMutation.activeMutation || appliedStatusSelectionRef.current === statusSelectionKey) return;
    appliedStatusSelectionRef.current = statusSelectionKey;
    const nextID = status?.database_id || databases[0]?.id || "default";
    setSelectedDatabaseID(nextID);
    const nextDatabase = databases.find((database) => database.id === nextID) || databases[0] || null;
    setActiveTab(nextDatabase ? "unlock" : "create");
  }, [statusSelectionKey, status?.database_id, databases, lifecycleMutation.activeMutation]);

  useEffect(
    () => () => {
      if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    },
    [],
  );

  const tabs: [Tab, string][] = [
    ...(hasDatabase ? [["unlock", "Unlock Database"] satisfies [Tab, string]] : []),
    ["create", hasDatabase ? "New Database" : "Create Database"],
    ["import", "Import Database"],
    ["remote", "Restore Remote"],
  ];
  function showToast(message: string) {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    setToast(message);
    toastTimerRef.current = window.setTimeout(() => {
      toastTimerRef.current = null;
      setToast("");
    }, 2400);
  }

  function handleDeleted(databaseID: string) {
    setMigrationRequiredIDs((current) => {
      const next = { ...current };
      delete next[databaseID];
      return next;
    });
    showToast("Local database deleted.");
  }

  return (
    <UnlockShell title={hasDatabase ? "Select database" : "Database setup"}>
      {toast ? <Toast message={toast} /> : null}
      <UnlockDatabasePicker
        databases={databases}
        selectedDatabase={selectedDatabase}
        disabled={Boolean(lifecycleMutation.activeMutation)}
        onSelect={(databaseID) => {
          setSelectedDatabaseID(databaseID);
          setActiveTab("unlock");
        }}
      />
      <UnlockStatusNotices
        sessionRequired={status?.state === "session_required"}
        unsupported={selectedUnsupported}
        migrationRequired={selectedMigrationRequired}
      />
      <UnlockTabs tabs={tabs} activeTab={activeTab} disabled={Boolean(lifecycleMutation.activeMutation)} onSelect={setActiveTab} />
      <UnlockActivePanel
        activeTab={activeTab}
        selectedDatabase={selectedDatabase}
        unsupported={selectedUnsupported}
        migrationRequired={selectedMigrationRequired}
        hasDatabase={hasDatabase}
        onMigrationRequired={(databaseID) => setMigrationRequiredIDs((current) => ({ ...current, [databaseID]: true }))}
        onDeleted={handleDeleted}
        runLifecycleMutation={lifecycleMutation.runMutation}
      />
    </UnlockShell>
  );
}

function UnlockDatabasePicker({
  databases,
  selectedDatabase,
  disabled,
  onSelect,
}: {
  databases: Database[];
  selectedDatabase: Database | null;
  disabled: boolean;
  onSelect: (_id: string) => void;
}) {
  if (databases.length === 0 || !selectedDatabase) return null;
  return (
    <div className="grid gap-2">
      <label htmlFor="unlock-database" className="text-sm font-semibold text-stone-800">
        Database
      </label>
      <select
        id="unlock-database"
        className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm outline-none focus:border-emerald-800"
        value={selectedDatabase.id}
        disabled={disabled}
        onChange={(event) => onSelect(event.target.value)}
      >
        {databases.map((database) => (
          <option key={database.id} value={database.id}>
            {database.name} {database.state === "unsupported_plaintext" ? "(unsupported plaintext)" : ""}
          </option>
        ))}
      </select>
    </div>
  );
}

function UnlockStatusNotices({
  sessionRequired,
  unsupported,
  migrationRequired,
}: {
  sessionRequired: boolean;
  unsupported: boolean;
  migrationRequired: boolean;
}) {
  return (
    <>
      {sessionRequired ? (
        <Notice tone="warn">Your browser session is missing or expired. Enter the database password to continue.</Notice>
      ) : null}
      {unsupported ? (
        <Notice tone="bad">
          This file is a plaintext SQLite database. AIPermission only supports SQLCipher-encrypted .aipdb databases.
        </Notice>
      ) : null}
      {migrationRequired ? (
        <Notice tone="warn">
          This database uses the pre-0.2 schema. Open the local migration helper, migrate it into a new 0.2 database, then delete this old
          local copy when you no longer need it.
        </Notice>
      ) : null}
    </>
  );
}

function UnlockTabs({
  tabs,
  activeTab,
  disabled,
  onSelect,
}: {
  tabs: [Tab, string][];
  activeTab: Tab;
  disabled: boolean;
  onSelect: (_tab: Tab) => void;
}) {
  return (
    <div className={`grid rounded-md border border-stone-200 bg-stone-100 p-1 ${unlockTabsGridClass(tabs.length)}`}>
      {tabs.map(([value, label], index) => (
        <button
          key={value}
          type="button"
          className={`min-h-10 whitespace-normal rounded px-2 py-2 text-xs font-semibold leading-tight transition sm:text-sm ${
            tabs.length % 2 === 1 && index === tabs.length - 1 ? "col-span-2 sm:col-span-1" : ""
          } ${activeTab === value ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
          disabled={disabled}
          onClick={() => onSelect(value)}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function UnlockActivePanel({
  activeTab,
  selectedDatabase,
  unsupported,
  migrationRequired,
  hasDatabase,
  onMigrationRequired,
  onDeleted,
  runLifecycleMutation,
}: ActivePanelProps) {
  if (activeTab === "create") return <UnlockCreatePanel hasDatabase={hasDatabase} runLifecycleMutation={runLifecycleMutation} />;
  if (activeTab === "import") return <UnlockImportPanel runLifecycleMutation={runLifecycleMutation} />;
  if (activeTab === "remote") return <RemoteRestorePanel runLifecycleMutation={runLifecycleMutation} />;
  return (
    <UnlockDatabasePanel
      key={selectedDatabase?.id}
      database={selectedDatabase}
      unsupported={unsupported}
      migrationRequired={migrationRequired}
      onMigrationRequired={onMigrationRequired}
      onDeleted={onDeleted}
      runLifecycleMutation={runLifecycleMutation}
    />
  );
}

function Toast({ message }: { message: string }) {
  return (
    <div
      role="status"
      aria-live="polite"
      className="fixed right-5 top-5 z-[80] rounded-md border border-stone-700 bg-stone-950 px-4 py-3 text-sm font-semibold text-white shadow-xl"
    >
      {message}
    </div>
  );
}

export function UnlockShell({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <main className="grid min-h-screen place-items-center bg-stone-100 p-5 text-stone-950">
      <div className="grid w-full max-w-2xl gap-2">
        <section className="grid gap-5 rounded-lg border border-stone-200 bg-white p-6 shadow-xl">
          <div className="flex items-center gap-3">
            <img src="/icon.svg" alt="" className="h-10 w-10 rounded-lg" />
            <div>
              <h1 className="text-lg font-semibold">aipermission</h1>
              <p className="text-sm text-stone-500">{title}</p>
            </div>
          </div>
          <Notice tone="warn">
            Local-only gateway. Keep Docker ports bound to <span className="font-mono">127.0.0.1</span>; do not expose this UI or API on LAN
            or the public internet.
          </Notice>
          {children}
        </section>
        <p className="text-center text-xs font-semibold text-stone-400">AIPermission {appVersion}</p>
      </div>
    </main>
  );
}
