import { KeyRound } from "lucide-react";
import { useState } from "react";
import { apiPost } from "../../lib/api";
import { isValidDatabasePassword } from "../../lib/password";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Field, Input } from "../ui/form";
import { Notice } from "../ui/notice";

const emptyPasswordForm = { current_password: "", new_password: "", confirm_password: "" };

export function PasswordSettingsPanel() {
  const [form, setForm] = useState(emptyPasswordForm);
  const { actionState, runAction } = useAsyncAction();
  const newPasswordValid = isValidDatabasePassword(form.new_password);

  function updateField(field, value) {
    setForm((current) => ({ ...current, [field]: value }));
  }

  async function changePassword(event) {
    event.preventDefault();
    await runAction({
      pending: "saving",
      successMessage: "Database password changed. Future unlocks and new backups use the new password.",
      action: async () => {
        await apiPost("/api/databases/change-password", form);
        setForm(emptyPasswordForm);
      },
    });
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Change password</CardTitle>
        <CardDescription>Re-encrypt the current database with a new unlock password.</CardDescription>
      </CardHeader>
      <CardContent>
        <form className="grid gap-4" onSubmit={changePassword}>
          <Notice>Downloaded backups keep the password they had when they were created.</Notice>
          <Field>
            Current password
            <Input
              type="password"
              value={form.current_password}
              onChange={(event) => updateField("current_password", event.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>
          <Field>
            New password
            <Input
              type="password"
              value={form.new_password}
              onChange={(event) => updateField("new_password", event.target.value)}
              autoComplete="new-password"
              minLength={14}
              required
            />
          </Field>
          <Field>
            Confirm new password
            <Input
              type="password"
              value={form.confirm_password}
              onChange={(event) => updateField("confirm_password", event.target.value)}
              autoComplete="new-password"
              minLength={14}
              required
            />
          </Field>
          <Notice>Use at least 14 characters with uppercase letters, lowercase letters, and numbers.</Notice>
          <Button
            type="submit"
            variant="outline"
            disabled={
              actionState.state === "saving" ||
              !newPasswordValid ||
              form.new_password !== form.confirm_password ||
              form.current_password === form.new_password
            }
          >
            <KeyRound className="h-4 w-4" />
            {actionState.state === "saving" ? "Changing..." : "Change password"}
          </Button>
          {actionState.message ? <Notice tone="good">{actionState.message}</Notice> : null}
          {actionState.state === "error" ? <Notice tone="bad">{actionState.error}</Notice> : null}
        </form>
      </CardContent>
    </Card>
  );
}
