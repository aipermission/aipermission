import { Link } from "react-router";
import { formatRelativeAge } from "../lib/date-time";
import { Notice } from "./ui/notice";

export function BackupFreshnessNotices({ value, onChange }) {
  return (
    <>
      {value.data.length > 0 ? (
        <Notice tone="warn" className="flex flex-wrap items-center justify-between gap-3">
          <span>
            A newer encrypted backup is available
            {value.data.length === 1
              ? ` from ${formatRelativeAge(value.data[0].latest_remote_at)}`
              : ` in ${value.data.length} providers`}
            . This local database may be stale.
          </span>
          <NoticeActions onDismiss={() => onChange((current) => ({ ...current, data: [] }))} />
        </Notice>
      ) : null}
      {value.checkErrors.length > 0 || value.error ? (
        <Notice tone="warn" className="flex flex-wrap items-center justify-between gap-3">
          <span>
            Backup freshness could not be checked
            {value.checkErrors.length > 0
              ? ` for ${value.checkErrors.length} provider${value.checkErrors.length === 1 ? "" : "s"}`
              : ""}
            . Review the provider connection before relying on the local copy.
          </span>
          <NoticeActions onDismiss={() => onChange((current) => ({ ...current, checkErrors: [], error: null }))} />
        </Notice>
      ) : null}
    </>
  );
}

function NoticeActions({ onDismiss }) {
  return (
    <span className="flex items-center gap-3">
      <Link className="font-semibold underline underline-offset-2" to="/settings">
        Review backups
      </Link>
      <button type="button" className="font-semibold text-stone-600 hover:text-stone-950" onClick={onDismiss}>
        Dismiss
      </button>
    </span>
  );
}
