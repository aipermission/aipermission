import { ConnectorTokenPermissionPanel } from "./connector-token-permission-panel";

export function TokenPermissionPanel({
  tokens,
  selectedTarget,
  targets,
  unreadMessages,
  compact = false,
  connectorPermissionState,
  loadAllConnectorPermissions,
  loadConnectorActions,
  replaceTokenConnectorPermissions,
  onToggleCompact,
  onRefresh,
  onOpenMessages,
}) {
  return (
    <ConnectorTokenPermissionPanel
      tokens={tokens}
      selectedTarget={selectedTarget}
      targets={targets}
      unreadMessages={unreadMessages}
      compact={compact}
      connectorPermissionState={connectorPermissionState}
      loadAllConnectorPermissions={loadAllConnectorPermissions}
      loadConnectorActions={loadConnectorActions}
      replaceTokenConnectorPermissions={replaceTokenConnectorPermissions}
      onToggleCompact={onToggleCompact}
      onRefresh={onRefresh}
      onOpenMessages={onOpenMessages}
    />
  );
}
