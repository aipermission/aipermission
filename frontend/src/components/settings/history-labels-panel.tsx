import { Tags, Trash2 } from "lucide-react";
import { useCallback, useEffect, useMemo, useState, type FormEvent } from "react";
import { apiDelete, apiGet } from "../../lib/api";
import { errorMessage } from "../../lib/errors";
import { useRequestGuard } from "../../lib/request-guard";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Dialog } from "../ui/dialog";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";

type HistoryLabel = { id: number; name: string };
type LabelResource = { state: "loading" | "ready" | "error"; data: HistoryLabel[]; error: string | null };

function historyLabelsResponse(value: unknown): HistoryLabel[] {
  if (value === null) return [];
  if (!Array.isArray(value)) throw new Error("History label response is invalid.");
  return value.map((entry: unknown) => {
    if (!entry || typeof entry !== "object" || !("id" in entry) || typeof entry.id !== "number" || !Number.isSafeInteger(entry.id) || entry.id <= 0 || !("name" in entry) || typeof entry.name !== "string") {
      throw new Error("History label response is invalid.");
    }
    return { id: entry.id, name: entry.name };
  });
}

export function HistoryLabelsPanel() {
  const [labels, setLabels] = useState<LabelResource>({ state: "loading", data: [], error: null });
  const requestGuard = useRequestGuard("history-labels-settings");
  const [selectedID, setSelectedID] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const { actionState, runAction } = useAsyncAction();
  const selectedLabel = useMemo(() => labels.data.find((label) => String(label.id) === String(selectedID)), [labels.data, selectedID]);

  const loadLabels = useCallback(async () => {
    const request = requestGuard.begin("labels");
    try {
      const data = historyLabelsResponse(await apiGet("/api/history-labels", { signal: request.signal }));
      if (request.isCurrent()) setLabels({ state: "ready", data, error: null });
    } catch (error) {
      if (request.isCurrent()) setLabels({ state: "error", data: [], error: errorMessage(error, "Unable to load history labels.") });
    } finally {
      request.complete();
    }
  }, [requestGuard]);

  useEffect(() => {
    void loadLabels();
  }, [loadLabels]);

  async function deleteLabel(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedLabel || actionState.state === "deleting") return;
    const deleted = selectedLabel;
    await runAction({
      pending: "deleting",
      successMessage: `Deleted history label "${deleted.name}".`,
      action: async () => {
        const request = requestGuard.begin("delete");
        try {
          await apiDelete(`/api/history-labels/${deleted.id}`);
          if (!request.isCurrent()) return;
          setSelectedID("");
          setDeleteOpen(false);
          await loadLabels();
        } finally {
          request.complete();
        }
      },
    });
  }

  function closeDelete() {
    if (actionState.state !== "deleting") setDeleteOpen(false);
  }

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>History labels</CardTitle>
          <CardDescription>Manage labels used to organize command history.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-4">
          <Notice>Deleting a label removes it from related history entries. The history records stay intact.</Notice>
          {labels.state === "error" ? <Notice tone="bad">{labels.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-[minmax(0,1fr)_auto]">
            <Select
              aria-label="History label"
              value={selectedID}
              onChange={(event) => setSelectedID(event.target.value)}
              disabled={labels.state === "loading" || labels.data.length === 0}
            >
              <option value="">{labels.state === "loading" ? "Loading labels..." : "Select a label"}</option>
              {labels.data.map((label) => (
                <option key={label.id} value={label.id}>
                  {label.name}
                </option>
              ))}
            </Select>
            <Button
              type="button"
              variant="outline"
              onClick={() => setDeleteOpen(true)}
              disabled={!selectedLabel || actionState.state === "deleting"}
            >
              <Trash2 className="h-4 w-4" />
              Delete label
            </Button>
          </div>
          {labels.state === "ready" && labels.data.length === 0 ? <Notice>No labels yet. Add labels from a history detail.</Notice> : null}
          {actionState.message ? <Notice tone="good">{actionState.message}</Notice> : null}
          {actionState.state === "error" ? <Notice tone="bad">{actionState.error}</Notice> : null}
        </CardContent>
      </Card>
      <Dialog
        open={deleteOpen}
        title="Delete history label"
        description={selectedLabel ? `Remove "${selectedLabel.name}" from history?` : "Select a history label first."}
        onClose={closeDelete}
        size="md"
      >
        <form className="grid gap-4" onSubmit={deleteLabel}>
          <Notice tone="bad">
            This removes the label from every related history entry. Command history records, outputs, and audit logs are not deleted.
          </Notice>
          <div className="rounded-md border border-stone-200 bg-stone-50 px-3 py-2">
            <p className="text-xs font-semibold uppercase text-stone-500">Selected label</p>
            <p className="mt-1 truncate text-sm font-semibold text-stone-950">{selectedLabel?.name || "-"}</p>
          </div>
          {actionState.state === "error" ? <Notice tone="bad">{actionState.error}</Notice> : null}
          <div className="grid gap-2 sm:grid-cols-2">
            <Button type="button" variant="outline" onClick={closeDelete} disabled={actionState.state === "deleting"}>
              Cancel
            </Button>
            <Button type="submit" variant="danger" disabled={!selectedLabel || actionState.state === "deleting"}>
              <Tags className="h-4 w-4" />
              {actionState.state === "deleting" ? "Deleting..." : "Delete label"}
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
