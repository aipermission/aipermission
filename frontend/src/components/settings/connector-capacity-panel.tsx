import { RefreshCcw } from "lucide-react";
import { formatBytes } from "../../lib/file-transfer-utils";
import type { ConnectorCapacityReport, ConnectorTokenCapacity } from "../../lib/gateway-contracts/connector-capacity-contract";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "../ui/card";
import { Notice } from "../ui/notice";
import { ProgressBar } from "../ui/progress-bar";
import { useConnectorCapacity } from "./use-connector-capacity";

const labels = { ok: "Available", warning: "80% used", critical: "90% used", exhausted: "Storage full" };

function TokenUsage({ item, report }: { item: ConnectorTokenCapacity; report: ConnectorCapacityReport }) {
  const bytes = item.stored_bytes + item.reserved_bytes;
  return (
    <li className="grid min-w-0 gap-2 border-b border-stone-200 py-3 last:border-0 dark:border-stone-800">
      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
        <span className="min-w-0 flex-1 truncate text-sm font-medium" title={item.name}>
          {item.name}
        </span>
        <Badge tone={item.level === "ok" ? "good" : item.level === "exhausted" ? "bad" : "warn"}>{labels[item.level]}</Badge>
      </div>
      <ProgressBar
        value={Math.max(bytes / report.byte_limit, item.rows / report.row_limit) * 100}
        label={`Request storage for ${item.name}`}
      />
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs text-stone-500 sm:grid-cols-4">
        <div>
          <dt>Stored</dt>
          <dd>{formatBytes(item.stored_bytes)}</dd>
        </div>
        <div>
          <dt>Reserved</dt>
          <dd>{formatBytes(item.reserved_bytes)}</dd>
        </div>
        <div>
          <dt>Records</dt>
          <dd>
            {item.rows.toLocaleString()} / {report.row_limit.toLocaleString()}
          </dd>
        </div>
        <div>
          <dt>Running / pending</dt>
          <dd>
            {item.running} / {item.pending}
          </dd>
        </div>
      </dl>
      {item.level === "exhausted" ? (
        <Notice tone="bad">
          New requests cannot fit within the storage budget. Review data retention or the operator storage limit. Waiting alone will not
          free retained records.
        </Notice>
      ) : null}
      {item.running >= report.running_limit ? (
        <Notice tone="warn">Concurrent work limit reached. Retry after a running request finishes.</Notice>
      ) : null}
    </li>
  );
}

export function ConnectorCapacityPanel() {
  const { data, loading, error, refresh } = useConnectorCapacity();
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>Connector capacity</CardTitle>
        <Button
          type="button"
          variant="outline"
          className="h-9 w-9 shrink-0 px-0"
          title="Refresh connector capacity"
          aria-label="Refresh connector capacity"
          disabled={loading}
          onClick={() => void refresh()}
        >
          <RefreshCcw className={`h-4 w-4 ${loading ? "animate-spin" : ""}`} />
        </Button>
      </CardHeader>
      <CardContent className="grid min-w-0 gap-3" aria-busy={loading}>
        {error ? (
          <div role="alert">
            <Notice tone="bad">
              {error}
              {data ? " Last successful reading shown; usage may have changed." : ""}
            </Notice>
          </div>
        ) : null}
        {!data ? (
          <div className="flex min-h-24 items-center justify-center text-sm text-stone-500">
            {loading ? "Loading capacity..." : "Capacity unavailable"}
          </div>
        ) : (
          <>
            <p className="text-xs text-stone-500">
              Per token: {formatBytes(data.byte_limit)}, {data.row_limit.toLocaleString()} records, {data.running_limit} running requests.
              Each new request needs {formatBytes(data.next_request_reservation_bytes)} of result headroom.
            </p>
            {data.history_days === 0 ? (
              <Notice tone="warn">History retention is disabled. Completed request records continue to accumulate.</Notice>
            ) : (
              <p className="text-xs text-stone-500">
                History retention: {data.history_days} days. Hourly cleanup removes eligible older records, not recent records.
              </p>
            )}
            {data.items.length === 0 ? (
              <div className="flex min-h-24 items-center justify-center text-sm text-stone-500">No unrevoked tokens</div>
            ) : (
              <ul className="max-h-96 overflow-auto">
                {data.items.map((item) => (
                  <TokenUsage key={item.token_id} item={item} report={data} />
                ))}
              </ul>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}
