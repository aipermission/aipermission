import { Tags, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { apiDelete, apiGet } from "../../lib/api";
import { useAsyncAction } from "../../lib/use-async-action";
import { Button } from "../ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "../ui/card";
import { Dialog } from "../ui/dialog";
import { Select } from "../ui/form";
import { Notice } from "../ui/notice";

export function HistoryLabelsPanel() {
  const [labels, setLabels] = useState({ state: "loading", data: [], error: null });
  const [selectedID, setSelectedID] = useState("");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const { actionState, runAction } = useAsyncAction();
  const selectedLabel = useMemo(() => labels.data.find((label) => String(label.id) === String(selectedID)), [labels.data, selectedID]);

  useEffect(() => {
    void loadLabels();
  }, []);

  async function loadLabels() {
    try {
      const data = await apiGet("/api/history-labels");
      setLabels({ state: "ready", data: data || [], error: null });
    } catch (error) {
      setLabels({ state: "error", data: [], error: error.message });
    }
  }

  async function deleteLabel(event) {
    event.preventDefault();
    if (!selectedLabel) return;
    const deleted = selectedLabel;
    await runAction({
      pending: "deleting",
      successMessage: `Deleted history label "${deleted.name}".`,
      action: async () => {
        await apiDelete(`/api/history-labels/${deleted.id}`);
        setSelectedID("");
        setDeleteOpen(false);
        await loadLabels();
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
