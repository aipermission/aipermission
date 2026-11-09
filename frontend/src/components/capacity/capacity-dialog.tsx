import { useMemo, useState } from "react";
import { RefreshCw, X, LoaderCircle } from "lucide-react";
import { Dialog } from "../ui/dialog";
import { Button } from "../ui/button";
import { Notice } from "../ui/notice";
import { activityAge, idleLabel, idleOptions, sessionGroup, type Group, type Report, type Session } from "./model";

type Props = {
  data: Report | null;
  error: string | null;
  message: string | null;
  saving: boolean;
  onClose: () => void;
  onRefresh: () => void;
  onClean: (_items: Session[], _minutes: number) => Promise<void>;
};
const groups: { key: Group; label: string }[] = [
  { key: "all", label: "All sessions" },
  { key: "running", label: "Running commands" },
  { key: "connected", label: "Connected" },
  { key: "idle", label: "Idle" },
  { key: "other", label: "Other states" },
];

function sessionRows(data: Report | null, group: Group, minutes: number, oldestFirst: boolean, query: string) {
  return (data?.items || [])
    .filter((item) => group === "all" || sessionGroup(item, data!, minutes) === group)
    .filter((item) =>
      [item.target_name, item.name, item.owner, item.connector_kind, `#${item.id}`].some((value) => value.toLowerCase().includes(query)),
    )
    .sort((a, b) => (Date.parse(a.last_activity) - Date.parse(b.last_activity)) * (oldestFirst ? 1 : -1) || a.id - b.id);
}

export function CapacityDialog({ data, error, message, saving, onClose, onRefresh, onClean }: Props) {
  const [minutes, setMinutes] = useState(30);
  const [group, setGroup] = useState<Group>("all");
  const [search, setSearch] = useState("");
  const [oldestFirst, setOldestFirst] = useState(true);
  const [confirmation, setConfirmation] = useState<{ items: Session[]; minutes: number } | null>(null);
  const [selected, setSelected] = useState<Set<string> | null>(null);
  const identity = (item: Session) => `${item.id}:${item.generation}:${item.last_activity}`;
  const query = search.trim().toLowerCase();
  const idle = useMemo(() => data?.items.filter((item) => sessionGroup(item, data, minutes) === "idle") || [], [data, minutes]);
  const rows = useMemo(() => sessionRows(data, group, minutes, oldestFirst, query), [data, group, minutes, oldestFirst, query]);
  const candidates = idle.filter((item) => selected === null || selected.has(identity(item)));
  function changeMinutes(value: number) {
    setMinutes(value);
    setSelected(null);
  }
  async function confirm() {
    if (!confirmation || saving) return;
    await onClean(confirmation.items, confirmation.minutes);
    setConfirmation(null);
    setSelected(null);
  }
  return (
    <>
      <Dialog
        open
        title="Console capacity"
        size="xl"
        className="sm:max-w-6xl"
        bodyClassName="h-[min(640px,calc(100dvh-150px))] min-h-0 overflow-y-auto"
        onClose={onClose}
        closeDisabled={saving}
      >
        <div className="grid min-h-full gap-5 md:grid-cols-[230px_minmax(0,1fr)]">
          <aside className="min-w-0 border-b border-stone-200 pb-4 md:border-b-0 md:border-r md:pr-5">
            <dl className="mb-5 grid grid-cols-3 gap-2 text-center text-sm">
              {[
                ["Used", data?.used],
                ["Free", data?.available],
                ["Limit", data?.limit],
              ].map(([label, count]) => (
                <div key={label}>
                  <dt className="text-xs text-stone-500">{label}</dt>
                  <dd className="mt-1 text-lg font-semibold">{count ?? "-"}</dd>
                </div>
              ))}
            </dl>
            <div role="tablist" aria-label="Session groups" className="grid grid-cols-2 gap-1 md:grid-cols-1">
              {groups.map(({ key, label }) => (
                <button
                  key={key}
                  type="button"
                  role="tab"
                  aria-selected={group === key}
                  aria-controls="capacity-sessions"
                  id={`capacity-tab-${key}`}
                  onClick={() => setGroup(key)}
                  className={`flex min-h-10 items-center justify-between gap-2 rounded-md px-3 py-2 text-left text-sm ${group === key ? "bg-emerald-950 text-white" : "hover:bg-stone-100"}`}
                >
                  <span>{label}</span>
                  <span>
                    {key === "all"
                      ? (data?.used ?? 0)
                      : (data?.items.filter((item) => sessionGroup(item, data, minutes) === key).length ?? 0)}
                  </span>
                </button>
              ))}
            </div>
            <label className="mt-5 grid gap-2 text-sm">
              Idle threshold
              <select
                value={minutes}
                disabled={saving}
                onChange={(event) => changeMinutes(Number(event.target.value))}
                className="h-10 rounded-md border border-stone-300 bg-white px-3"
              >
                {idleOptions.map((value) => (
                  <option key={value} value={value}>
                    {idleLabel(value)}
                  </option>
                ))}
              </select>
            </label>
            <Button
              type="button"
              variant="outline"
              className="mt-3 w-full text-xs"
              disabled={!idle.length || saving || !!error}
              onClick={() => setSelected(new Set(idle.map(identity)))}
            >
              Select idle ({idle.length})
            </Button>
            <Button
              type="button"
              variant="danger"
              className="mt-2 w-full text-xs"
              disabled={!candidates.length || saving || !!error}
              onClick={() => setConfirmation({ items: candidates, minutes })}
            >
              {saving ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <X className="h-4 w-4" />}Clean {candidates.length} sessions
            </Button>
          </aside>
          <section role="tabpanel" id="capacity-sessions" aria-labelledby={`capacity-tab-${group}`} className="min-w-0">
            <div className="mb-4 flex flex-wrap items-center justify-end gap-2">
              <input
                type="search"
                aria-label="Search sessions"
                placeholder="Search sessions"
                maxLength={200}
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                className="h-9 min-w-0 basis-full rounded-md border border-stone-300 bg-white px-3 text-sm sm:flex-1 sm:basis-0"
              />
              <select
                aria-label="Session order"
                value={oldestFirst ? "oldest" : "newest"}
                onChange={(event) => setOldestFirst(event.target.value === "oldest")}
                className="h-9 min-w-0 rounded-md border border-stone-300 bg-white px-2 text-sm"
              >
                <option value="oldest">Oldest activity first</option>
                <option value="newest">Newest activity first</option>
              </select>
              <Button
                type="button"
                variant="outline"
                className="h-9 w-9 shrink-0 px-0"
                title="Refresh capacity"
                aria-label="Refresh capacity"
                onClick={onRefresh}
              >
                <RefreshCw className="h-4 w-4" />
              </Button>
            </div>
            {error ? <Notice tone="bad">{error}</Notice> : null}
            {message ? <Notice>{message}</Notice> : null}
            {!data ? (
              <Notice>Loading capacity...</Notice>
            ) : (
              <div className="max-h-[480px] overflow-auto">
                <table className="w-full text-left text-sm">
                  <thead className="sticky top-0 bg-white">
                    <tr>
                      <th className="p-2">
                        <span className="sr-only">Select</span>
                      </th>
                      <th className="p-2">Connector / session</th>
                      <th className="p-2">Owner</th>
                      <th className="p-2">Status</th>
                      <th className="p-2">Last activity</th>
                      <th className="p-2">
                        <span className="sr-only">Actions</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {rows.map((item) => {
                      const currentGroup = sessionGroup(item, data, minutes);
                      return (
                        <tr key={item.id} className="border-t border-stone-200">
                          <td className="p-2">
                            <input
                              type="checkbox"
                              aria-label={`Select session ${item.id}`}
                              checked={currentGroup === "idle" && (selected === null || selected.has(identity(item)))}
                              disabled={currentGroup !== "idle" || saving || !!error}
                              onChange={(event) =>
                                setSelected((previous) => {
                                  const next = new Set(previous ?? idle.map(identity));
                                  if (event.target.checked) next.add(identity(item));
                                  else next.delete(identity(item));
                                  return next;
                                })
                              }
                            />
                          </td>
                          <td className="max-w-64 break-words p-2">
                            <span className="font-semibold">{item.target_name || item.name}</span>
                            <div className="text-xs text-stone-500">
                              {item.connector_kind} / #{item.id} / {item.name}
                            </div>
                          </td>
                          <td className="break-all p-2 text-xs">{item.owner}</td>
                          <td className="p-2">
                            <span>{currentGroup === "idle" ? "Idle" : item.state}</span>
                            <div className="text-xs text-stone-500">{item.clients} viewers</div>
                          </td>
                          <td className="whitespace-nowrap p-2" title={item.last_activity}>
                            {activityAge(item, data)}
                          </td>
                          <td className="p-2">
                            <Button
                              type="button"
                              variant="outline"
                              className="h-8 w-8 px-0"
                              title={`Close session ${item.id}`}
                              aria-label={`Close session ${item.id}`}
                              disabled={!item.can_close || saving || !!error}
                              onClick={() => setConfirmation({ items: [item], minutes: 0 })}
                            >
                              <X className="h-4 w-4" />
                            </Button>
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
                {rows.length === 0 ? <Notice>{query ? "No sessions match your search." : "No sessions in this group."}</Notice> : null}
              </div>
            )}
          </section>
        </div>
      </Dialog>
      {confirmation ? (
        <Dialog
          open
          title={`Close ${confirmation.items.length} console sessions?`}
          onClose={() => setConfirmation(null)}
          closeDisabled={saving}
        >
          <p className="text-sm text-stone-600">
            Closing a shell disconnects its viewers and may affect remote work. Inactivity does not prove that detached processes have
            stopped.
          </p>
          <ul className="my-4 max-h-40 overflow-auto text-sm">
            {confirmation.items.map((item) => (
              <li key={item.id}>
                {item.target_name || item.name} / #{item.id} / {item.owner}
              </li>
            ))}
          </ul>
          <div className="flex justify-end gap-2">
            <Button variant="outline" disabled={saving} onClick={() => setConfirmation(null)}>
              Cancel
            </Button>
            <Button variant="danger" disabled={saving} onClick={() => void confirm()}>
              {saving ? "Closing..." : "Close sessions"}
            </Button>
          </div>
        </Dialog>
      ) : null}
    </>
  );
}
