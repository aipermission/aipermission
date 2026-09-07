import { Button } from "../../../components/ui/button";
import { Dialog } from "../../../components/ui/dialog";
import { Notice } from "../../../components/ui/notice";

export function RedisConfirmDialog({ value, theme, product, onClose, onConfirm }) {
  const danger = value.tone === "bad";
  const detailClass = theme === "light" ? "bg-stone-50" : "bg-stone-900/70 text-stone-100";
  return (
    <Dialog
      open={value.open}
      title={value.title}
      description={value.description}
      onClose={onClose}
      closeDisabled={value.pending}
      size="md"
      closeOnOverlay={!value.pending}
      closeOnEscape={!value.pending}
      className={theme === "light" ? "" : "border-stone-700 bg-[#252526] text-stone-100"}
      bodyClassName={theme === "light" ? "" : "bg-[#252526]"}
    >
      <div className="grid gap-4">
        <Notice tone={danger ? "bad" : "warn"}>
          {danger ? "This operation cannot be undone." : `Review the ${product} write before continuing.`}
        </Notice>
        {value.error ? <Notice tone="bad">{value.error}</Notice> : null}
        {value.details?.length ? (
          <div className={`max-h-56 overflow-auto rounded-md border border-stone-300 p-3 text-sm ${detailClass}`}>
            {value.details.map((item, index) => (
              <div key={`${item.label}:${index}`} className="grid gap-1 py-1 sm:grid-cols-[110px_minmax(0,1fr)]">
                <span className="text-xs font-semibold uppercase text-stone-500">{item.label}</span>
                <span className="break-all font-mono text-xs">{item.value}</span>
              </div>
            ))}
          </div>
        ) : null}
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose} disabled={value.pending}>
            Cancel
          </Button>
          <Button type="button" variant={danger ? "danger" : "default"} onClick={onConfirm} disabled={value.pending}>
            {value.pending ? "Working..." : danger ? "Delete" : "Confirm"}
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
