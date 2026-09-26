import { useCallback, useMemo, useState } from "react";
import { useFamilyCommandHost } from "./use-family-command-host";
import type { InventoryProfile, InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import type { ConnectorFamilyCommands } from "./connector-family-types";
import type { ConnectorTestState } from "./use-connector-connection-tests";

export function useConnectorFamilyHost() {
  const { commands, ...host } = useFamilyCommandHost<ConnectorFamilyCommands>();
  const [familyTests, setFamilyTests] = useState<ReadonlyMap<string, Record<string, ConnectorTestState>>>(new Map());
  const onTestsChange = useCallback((kind: string, tests: Record<string, ConnectorTestState> | null) => {
    setFamilyTests((current) => {
      if ((tests === null && !current.has(kind)) || current.get(kind) === tests) return current;
      const next = new Map(current);
      if (tests === null) next.delete(kind);
      else next.set(kind, tests);
      return next;
    });
  }, []);
  const tests = useMemo(() => {
    const combined: Record<string, ConnectorTestState> = {};
    for (const results of familyTests.values()) for (const [key, value] of Object.entries(results)) combined[key] = value;
    return combined;
  }, [familyTests]);
  const openCreate = useCallback(
    (kind: string, projectID?: string | number | null) => commands.current.get(kind)?.openCreate(projectID ?? undefined),
    [commands],
  );
  const openEdit = useCallback(
    (target: InventoryTarget, profile: InventoryProfile | null) => commands.current.get(target.connector_kind)?.openEdit(target, profile),
    [commands],
  );
  const requestDelete = useCallback(
    (target: InventoryTarget) => commands.current.get(target.connector_kind)?.requestDelete(target),
    [commands],
  );
  const test = useCallback(
    (target: InventoryTarget, profile: InventoryProfile | null) => commands.current.get(target.connector_kind)?.test(target, profile),
    [commands],
  );
  return { ...host, tests, onTestsChange, openCreate, openEdit, requestDelete, test };
}
