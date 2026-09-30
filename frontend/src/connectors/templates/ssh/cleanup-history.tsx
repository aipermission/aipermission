import { Copy } from "lucide-react";
import { CopyButton } from "../../../components/ui/copy-button";
import type { CleanupEntry } from "./cleanup-types";

export function CleanupHistory({ entry }: { entry: CleanupEntry }) {
  return (
    <section className="grid min-w-0 gap-3 border-t border-stone-200 pt-3" aria-label="Recorded cleanup evidence">
      <h3 className="text-sm font-semibold">Recorded cleanup evidence</h3>
      <details>
        <summary className="cursor-pointer text-sm">Original identity</summary>
        <CleanupJSON label="Original cleanup identity JSON" value={entry.record.identity} />
      </details>
      {entry.record.attestations.length === 0 ? (
        <p className="text-sm text-stone-500">No recorded operator decisions.</p>
      ) : (
        entry.record.attestations.map((proof, index) => (
          <details key={index}>
            <summary className="cursor-pointer break-all text-sm">
              Decision {index + 1}: {proof.identity.username}@{proof.identity.host}:{proof.identity.port}
            </summary>
            <div className="mt-2 flex min-w-0 items-center gap-2">
              <p className="min-w-0 flex-1 whitespace-pre-wrap break-words text-sm">{proof.reason}</p>
              <CopyButton
                value={JSON.stringify(proof, null, 2)}
                variant="outline"
                className="h-8 w-8 px-0"
                title={`Copy recorded decision ${index + 1}`}
              >
                <Copy className="h-4 w-4" />
              </CopyButton>
            </div>
            <CleanupJSON label={`Recorded decision ${index + 1} JSON`} value={proof} />
          </details>
        ))
      )}
    </section>
  );
}

function CleanupJSON({ label, value }: { label: string; value: unknown }) {
  return (
    <textarea
      aria-label={label}
      readOnly
      wrap="off"
      rows={12}
      value={JSON.stringify(value, null, 2)}
      className="mt-2 block max-h-64 w-full resize-none overflow-auto rounded border border-stone-200 p-3 font-mono text-xs"
    />
  );
}
