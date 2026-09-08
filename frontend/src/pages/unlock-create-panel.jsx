import { useState } from "react";
import { LockKeyhole } from "lucide-react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/form";
import { Notice } from "../components/ui/notice";
import { apiPost } from "../lib/api";
import { isValidDatabasePassword } from "../lib/password";

export function UnlockCreatePanel({ hasDatabase, runLifecycleMutation }) {
  const [form, setForm] = useState({ database_name: "", password: "", confirm_password: "" });
  const [state, setState] = useState({ state: "idle", error: null });
  const passwordValid = isValidDatabasePassword(form.password);

  async function createDatabase(event) {
    event.preventDefault();
    setState({ state: "saving", error: null });
    try {
      await runLifecycleMutation("create", (signal) =>
        apiPost(
          "/api/unlock/setup",
          {
            password: form.password,
            confirm_password: form.confirm_password,
            database_name: form.database_name,
          },
          { signal },
        ),
      );
    } catch (error) {
      setState({ state: "error", error: error.message });
    }
  }

  return (
    <form className="grid gap-4" onSubmit={createDatabase}>
      <div className="grid gap-2">
        <label htmlFor="create-database-name" className="text-sm font-semibold text-stone-800">
          Database name
        </label>
        <Input
          id="create-database-name"
          type="text"
          value={form.database_name}
          onChange={(event) => setForm((current) => ({ ...current, database_name: event.target.value }))}
          placeholder={hasDatabase ? "Project name" : "Default"}
          required={hasDatabase}
        />
      </div>
      <div className="grid gap-2">
        <label htmlFor="create-database-password" className="text-sm font-semibold text-stone-800">
          Database password
        </label>
        <Input
          id="create-database-password"
          type="password"
          value={form.password}
          onChange={(event) => setForm((current) => ({ ...current, password: event.target.value }))}
          minLength={14}
          autoFocus={!hasDatabase}
          required
        />
      </div>
      <div className="grid gap-2">
        <label htmlFor="create-database-password-confirmation" className="text-sm font-semibold text-stone-800">
          Confirm password
        </label>
        <Input
          id="create-database-password-confirmation"
          type="password"
          value={form.confirm_password}
          onChange={(event) => setForm((current) => ({ ...current, confirm_password: event.target.value }))}
          minLength={14}
          required
        />
      </div>
      <Notice>Use at least 14 characters with uppercase letters, lowercase letters, and numbers. This password cannot be recovered.</Notice>
      {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}
      <Button type="submit" disabled={state.state === "saving" || !passwordValid || form.password !== form.confirm_password}>
        <LockKeyhole className="h-4 w-4" />
        {state.state === "saving" ? "Working..." : "Create encrypted database"}
      </Button>
    </form>
  );
}
