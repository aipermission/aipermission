import { useCallback, useState } from "react";
import type { CredentialFamilyCommands } from "./credential-family-types";
import { useFamilyCommandHost } from "./use-family-command-host";

export function useCredentialFamilyHost() {
  const { commands, ...host } = useFamilyCommandHost<CredentialFamilyCommands>();
  const [rowCounts, setRowCounts] = useState<ReadonlyMap<string, number>>(new Map());
  const onRowsChange = useCallback((kind: string, count: number | null) => {
    setRowCounts((current) => {
      if ((count === null && !current.has(kind)) || current.get(kind) === count) return current;
      const next = new Map(current);
      if (count === null) next.delete(kind);
      else next.set(kind, count);
      return next;
    });
  }, []);
  const openCreate = useCallback((kind: string) => commands.current.get(kind)?.openCreate(), [commands]);
  return {
    ...host,
    rowCounts,
    onRowsChange,
    openCreate,
  };
}
