import { CheckCircle2, LockKeyhole } from "lucide-react";
import { Button } from "./ui/button";
import { Dialog } from "./ui/dialog";
import { Input } from "./ui/form";
import { Notice } from "./ui/notice";

export function DatabaseSwitchDialog({ state, databaseStatus, onChange, onClose, onSubmit }) {
  const databases = databaseStatus?.databases || [];
  const currentID = databaseStatus?.database_id || "";
  const selected = databases.find((database) => database.id === state.database_id) || null;
  const selectedIsCurrent = selected?.id === currentID;
  const selectedIsUnlocked = Boolean(selected?.unlocked);

  return (
    <Dialog
      open={state.open}
      title="Switch database"
      description="Continue with the current database, or unlock another one."
      onClose={onClose}
      size="md"
    >
      <form className="grid gap-4" onSubmit={onSubmit}>
        <div className="grid gap-2">
          {databases.map((database) => {
            const current = database.id === currentID;
            const unlocked = Boolean(database.unlocked);
            const selectedRow = database.id === state.database_id;
            return (
              <button
                key={database.id}
                type="button"
                className={`flex items-center justify-between gap-3 rounded-md border p-3 text-left transition ${
                  selectedRow ? "border-emerald-700 bg-emerald-50" : "border-stone-200 bg-white hover:bg-stone-50"
                }`}
                onClick={() => onChange((dialog) => ({ ...dialog, database_id: database.id, password: "", error: null, state: "idle" }))}
              >
                <span className="min-w-0">
                  <span className="block truncate text-sm font-semibold text-stone-900">{database.name}</span>
                  <span className="block truncate text-xs text-stone-500">{database.id}</span>
                </span>
                {current || unlocked ? (
                  <CheckCircle2 className="h-5 w-5 shrink-0 text-emerald-700" />
                ) : (
                  <LockKeyhole className="h-5 w-5 shrink-0 text-stone-400" />
                )}
              </button>
            );
          })}
        </div>

        {!selectedIsCurrent && !selectedIsUnlocked ? (
          <div className="grid gap-2">
            <label className="text-sm font-semibold text-stone-800">Database password</label>
            <Input
              type="password"
              value={state.password}
              onChange={(event) => onChange((dialog) => ({ ...dialog, password: event.target.value }))}
              autoFocus
              required
            />
          </div>
        ) : null}

        {state.state === "error" ? <Notice tone="bad">{state.error}</Notice> : null}

        <Button type="submit" disabled={!selected || state.state === "switching"}>
          {selectedIsCurrent
            ? "Continue"
            : selectedIsUnlocked
              ? "Switch"
              : state.state === "switching"
                ? "Unlocking..."
                : "Unlock and switch"}
        </Button>
      </form>
    </Dialog>
  );
}
