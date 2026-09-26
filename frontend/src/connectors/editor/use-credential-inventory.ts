import { useEffect, useEffectEvent, useState } from "react";
import { connectorCatalogResponse, type ConnectorCatalogItem } from "../../lib/gateway-contracts/connector-catalog-contract";
import { connectorInventoryResponse, type InventoryTarget } from "../../lib/gateway-contracts/connector-inventory-contract";
import { loadGatewayCollection, type GatewayCollection } from "../../lib/load-gateway-collection";
import { useRequestGuard } from "../../lib/request-guard";

export function useCredentialInventory() {
  const [catalog, setCatalog] = useState<GatewayCollection<ConnectorCatalogItem>>({ state: "loading", data: [], error: null });
  const [targets, setTargets] = useState<GatewayCollection<InventoryTarget>>({ state: "loading", data: [], error: null });
  const guard = useRequestGuard("credential-inventory");
  const initialize = useEffectEvent(() => {
    void refresh();
  });
  useEffect(() => {
    initialize();
  }, []);

  async function refresh() {
    await Promise.all([
      loadGatewayCollection({ path: "/api/connectors", channel: "catalog", guard, decode: connectorCatalogResponse, setState: setCatalog }),
      loadGatewayCollection({
        path: "/api/connector-targets/inventory",
        channel: "targets",
        guard,
        decode: connectorInventoryResponse,
        setState: setTargets,
      }),
    ]);
  }

  return { catalog, targets, refresh };
}
