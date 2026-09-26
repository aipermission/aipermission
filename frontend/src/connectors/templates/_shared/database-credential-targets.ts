import type { InventoryTarget } from "../../../lib/gateway-contracts/connector-inventory-contract";
import { optionalConsolePort, optionalConsoleText } from "./console-target-config";

export function databaseCredentialTargets(targets: readonly InventoryTarget[], kind: string, label: string) {
  return targets
    .filter((target) => target.connector_kind === kind)
    .map((target) => {
      const config = target.config || {};
      return {
        ...target,
        config: {
          ...config,
          host: optionalConsoleText(config.host, label, "host"),
          port: optionalConsolePort(config.port, label),
          database: optionalConsoleText(config.database, label, "database"),
          connection_mode: optionalConsoleText(config.connection_mode, label, "connection_mode"),
          transport_target_ref: optionalConsoleText(config.transport_target_ref, label, "transport_target_ref"),
        },
        profiles: (target.profiles || []).map((profile) => {
          const publicMetadata: Record<string, unknown> & { username?: string } = {
            ...profile.public,
            username: optionalConsoleText(profile.public?.username, label, "username"),
          };
          return { ...profile, public: publicMetadata };
        }),
      };
    });
}
