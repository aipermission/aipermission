import { useState } from "react";
import { Upload } from "lucide-react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiPostForm } from "../lib/api";
import { useRequestGuard } from "../lib/request-guard";

export function UnlockImportPanel({ onUnlocked }) {
  const [form, setForm] = useState({ database_name: "", file: null, database_password: "" });
  const [state, setState] = useState({ state: "idle", error: null });
  const requestGuard = useRequestGuard("unlock:import");

  async function importDatabase(event) {
    event.preventDefault();
    if (!form.file) {
      setState({ state: "error", error: "Database file is required" });
      return;
    }
    const request = requestGuard.begin("submit");
    setState({ state: "importing", error: null });
    try {
      const formData = new FormData();
      formData.set("sqlite", form.file, form.file.name);
      formData.set("database_password", form.database_password);
      formData.set("database_name", form.database_name);
      await apiPostForm("/api/backup/import", formData, { signal: request.signal });
      if (request.isCurrent()) await onUnlocked();
    } catch (error) {
      if (request.isCurrent()) setState({ state: "error", error: error.message });
    } finally {
      request.complete();
    }
  }

  return (
    <form className="grid gap-4" onSubmit={importDatabase}>
      <div>
        <h2 className="text-sm font-semibold text-stone-900">Import encrypted database</h2>
        <p className="mt-1 text-sm text-stone-500">
          Choose an exported .aipdb or SQLCipher .db file, then enter that database password. Imports always create a new named database.
        </p>
      </div>
      <div className="grid gap-2">
        <label htmlFor="import-database-name" className="text-sm font-semibold text-stone-800">
          Database name
        </label>
        <Input
          id="import-database-name"
          type="text"
          value={form.database_name}
          onChange={(event) => setForm((current) => ({ ...current, database_name: event.target.value }))}
          placeholder="Restored project"
          required
        />
      </div>
      <div className="grid gap-2">
        <label htmlFor="import-database-file" className="text-sm font-semibold text-stone-800">
          Database file
        </label>
        <Input
          id="import-database-file"
          type="file"
          accept=".aipdb,.db,application/octet-stream"
          onChange={(event) => setForm((current) => ({ ...current, file: event.target.files?.[0] || null }))}
          required
        />
      </div>
      <div className="grid gap-2">
        <label htmlFor="import-database-password" className="text-sm font-semibold text-stone-800">
          Database password
        </label>
        <Input
          id="import-database-password"
          type="password"
          value={form.database_password}
          onChange={(event) => setForm((current) => ({ ...current, database_password: event.target.value }))}
          required
        />
      </div>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" variant="outline" disabled={state.state === "importing"}>
        <Upload className="h-4 w-4" />
        {state.state === "importing" ? "Importing..." : "Import database"}
      </Button>
    </form>
  );
}
