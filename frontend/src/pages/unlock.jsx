import { useEffect, useMemo, useRef, useState } from "react";
import { Notice } from "../components/ui/notice";
import { appVersion } from "../lib/release";
import { RemoteRestorePanel } from "./remote-restore-panel";
import { UnlockCreatePanel } from "./unlock-create-panel";
import { UnlockDatabasePanel } from "./unlock-database-panel";
import { UnlockImportPanel } from "./unlock-import-panel";

function unlockTabsGridClass(tabCount) {
  return tabCount === 4 ? "grid-cols-2 sm:grid-cols-4" : "grid-cols-2 sm:grid-cols-3";
}

export function UnlockPage({ status, onUnlocked }) {
  const databases = useMemo(() => status?.databases || [], [status?.databases]);
  const firstDatabaseID = status?.database_id || databases[0]?.id || "default";
  const [selectedDatabaseID, setSelectedDatabaseID] = useState(firstDatabaseID);
  const selectedDatabase = databases.find((database) => database.id === selectedDatabaseID) || databases[0] || null;
  const hasDatabase = Boolean(selectedDatabase);
  const selectedUnsupported = selectedDatabase?.state === "unsupported_plaintext";
  const [migrationRequiredIDs, setMigrationRequiredIDs] = useState({});
  const selectedMigrationRequired = Boolean(selectedDatabase && migrationRequiredIDs[selectedDatabase.id]);
  const [activeTab, setActiveTab] = useState(hasDatabase ? "unlock" : "create");
  const [toast, setToast] = useState("");
  const toastTimerRef = useRef(null);

  useEffect(() => {
    const nextID = status?.database_id || databases[0]?.id || "default";
    setSelectedDatabaseID(nextID);
  }, [status?.database_id, databases]);

  useEffect(() => {
    if (!selectedDatabase) {
      setActiveTab("create");
      return;
    }
    setActiveTab("unlock");
  }, [selectedDatabase]);

  useEffect(
    () => () => {
      if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    },
    [],
  );

  const tabs = [
    ...(hasDatabase ? [["unlock", "Unlock Database"]] : []),
    ["create", hasDatabase ? "New Database" : "Create Database"],
    ["import", "Import Database"],
    ["remote", "Restore Remote"],
  ];
  function showToast(message) {
    if (toastTimerRef.current !== null) window.clearTimeout(toastTimerRef.current);
    setToast(message);
    toastTimerRef.current = window.setTimeout(() => {
      toastTimerRef.current = null;
      setToast("");
    }, 2400);
  }

  async function handleDeleted(databaseID) {
    setMigrationRequiredIDs((current) => {
      const next = { ...current };
      delete next[databaseID];
      return next;
    });
    showToast("Local database deleted.");
    await onUnlocked();
  }

  return (
    <UnlockShell title={hasDatabase ? "Select database" : "Database setup"}>
      {toast ? <Toast message={toast} /> : null}
      <UnlockDatabasePicker
        databases={databases}
        selectedDatabase={selectedDatabase}
        selectedDatabaseID={selectedDatabaseID}
        onSelect={setSelectedDatabaseID}
      />
      <UnlockStatusNotices
        sessionRequired={status?.state === "session_required"}
        unsupported={selectedUnsupported}
        migrationRequired={selectedMigrationRequired}
      />
      <UnlockTabs tabs={tabs} activeTab={activeTab} onSelect={setActiveTab} />
      <UnlockActivePanel
        activeTab={activeTab}
        selectedDatabase={selectedDatabase}
        unsupported={selectedUnsupported}
        migrationRequired={selectedMigrationRequired}
        hasDatabase={hasDatabase}
        onMigrationRequired={(databaseID) => setMigrationRequiredIDs((current) => ({ ...current, [databaseID]: true }))}
        onDeleted={handleDeleted}
        onUnlocked={onUnlocked}
      />
    </UnlockShell>
  );
}

function UnlockDatabasePicker({ databases, selectedDatabase, selectedDatabaseID, onSelect }) {
  if (databases.length === 0) return null;
  return (
    <div className="grid gap-2">
      <label htmlFor="unlock-database" className="text-sm font-semibold text-stone-800">
        Database
      </label>
      <select
        id="unlock-database"
        className="h-10 rounded-md border border-stone-300 bg-white px-3 text-sm outline-none focus:border-emerald-800"
        value={selectedDatabase?.id || selectedDatabaseID}
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

function UnlockStatusNotices({ sessionRequired, unsupported, migrationRequired }) {
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

function UnlockTabs({ tabs, activeTab, onSelect }) {
  return (
    <div className={`grid rounded-md border border-stone-200 bg-stone-100 p-1 ${unlockTabsGridClass(tabs.length)}`}>
      {tabs.map(([value, label], index) => (
        <button
          key={value}
          type="button"
          className={`min-h-10 whitespace-normal rounded px-2 py-2 text-xs font-semibold leading-tight transition sm:text-sm ${
            tabs.length % 2 === 1 && index === tabs.length - 1 ? "col-span-2 sm:col-span-1" : ""
          } ${activeTab === value ? "bg-white text-emerald-950 shadow-sm" : "text-stone-500 hover:text-stone-900"}`}
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
  onUnlocked,
}) {
  if (activeTab === "create") return <UnlockCreatePanel hasDatabase={hasDatabase} onUnlocked={onUnlocked} />;
  if (activeTab === "import") return <UnlockImportPanel onUnlocked={onUnlocked} />;
  if (activeTab === "remote") return <RemoteRestorePanel onUnlocked={onUnlocked} />;
  if (activeTab !== "unlock") return null;
  return (
    <UnlockDatabasePanel
      key={selectedDatabase?.id}
      database={selectedDatabase}
      unsupported={unsupported}
      migrationRequired={migrationRequired}
      onMigrationRequired={onMigrationRequired}
      onDeleted={onDeleted}
      onUnlocked={onUnlocked}
    />
  );
}

function Toast({ message }) {
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

export function UnlockShell({ title, children }) {
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
