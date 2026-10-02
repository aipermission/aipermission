import { useEffect, useEffectEvent, useMemo, useRef, useState } from "react";
import { apiPost } from "../../lib/api";
import { useGateway } from "../../lib/gateway-context";
import { useAsyncAction } from "../../lib/use-async-action";
import { useConnectorPermissions } from "../../lib/use-connector-permissions";
import { tokenStatus } from "../../lib/token-status";
import { useTokenExpiryClock } from "../../lib/use-token-expiry-clock";
import { emptyForm, tokenCreatePayload } from "./token-create-drawer";
import {
  gatewayCreatedTokenResponse,
  type CreatedGatewayToken,
  type GatewayToken,
} from "../../lib/gateway-contracts/core-resource-contracts";
import type { FormEvent } from "react";
import type { TokenInstallState } from "./token-install-dialog";

export type TokenStatistics = { total: number; active: number; expired: number; revoked: number };

export function useTokenPageController() {
  const { tokens, loadTokens, loadTargets } = useGateway();
  const issuance = useTokenIssuance(loadTokens);
  const state = issuance.actionState;
  const [connectorPermissionDialog, setConnectorPermissionDialog] = useState<GatewayToken | null>(null);
  const [vaultPermissionDialog, setVaultPermissionDialog] = useState<GatewayToken | null>(null);
  const { connectorPermissionState, loadAllConnectorPermissions } = useConnectorPermissions(tokens.data);
  const [installDialog, setInstallDialog] = useState<TokenInstallState>({ open: false, token: null, provider: "manual" });
  const [revokeDialog, setRevokeDialog] = useState<GatewayToken | null>(null);
  const { actionState: revokeState, runAction: runRevokeAction, resetAction: resetRevokeAction } = useAsyncAction();
  const [tokenFilter, setTokenFilter] = useState("active");
  const loadPermissionsForEffect = useEffectEvent(() => loadAllConnectorPermissions(tokens.data));
  const tokenNow = useTokenExpiryClock(tokens.data);

  const stats = useMemo(() => {
    const active = tokens.data.filter((token) => tokenStatus(token, tokenNow) === "active").length;
    const expired = tokens.data.filter((token) => tokenStatus(token, tokenNow) === "expired").length;
    return {
      total: tokens.data.length,
      active,
      expired,
      revoked: tokens.data.filter((token) => Boolean(token.revoked_at)).length,
    };
  }, [tokens.data, tokenNow]);

  const visibleTokens = useMemo(() => {
    if (tokenFilter === "active") return tokens.data.filter((token) => tokenStatus(token, tokenNow) === "active");
    if (tokenFilter === "expired") return tokens.data.filter((token) => tokenStatus(token, tokenNow) === "expired");
    if (tokenFilter === "revoked") return tokens.data.filter((token) => Boolean(token.revoked_at));
    return tokens.data;
  }, [tokenFilter, tokens.data, tokenNow]);

  const tokenIDs = tokens.data.map((token) => token.id).join(",");
  useEffect(() => {
    if (tokens.state !== "ready") return;
    loadPermissionsForEffect();
  }, [tokens.state, tokenIDs]);

  async function refreshTokensAndPermissions() {
    const tokenItems = await loadTokens();
    await Promise.all([loadTargets(), loadAllConnectorPermissions(tokenItems)]);
  }

  async function revokeToken(token: GatewayToken | null) {
    if (!token) return;
    const revoked = await runRevokeAction({
      pending: "revoking",
      successMessage: `${token.name} revoked.`,
      action: async () => {
        await apiPost(`/api/tokens/${token.id}/revoke`, {});
        await loadTokens();
        return true;
      },
    });
    if (revoked === true) setRevokeDialog(null);
  }

  function openRevokeDialog(token: GatewayToken) {
    resetRevokeAction();
    setRevokeDialog(token);
  }

  function closeRevokeDialog() {
    resetRevokeAction();
    setRevokeDialog(null);
  }

  return {
    tokens,
    issuance,
    state,
    connectorPermissionDialog,
    setConnectorPermissionDialog,
    vaultPermissionDialog,
    setVaultPermissionDialog,
    connectorPermissionState,
    loadAllConnectorPermissions,
    installDialog,
    setInstallDialog,
    revokeDialog,
    revokeState,
    tokenFilter,
    setTokenFilter,
    tokenNow,
    stats,
    visibleTokens,
    refreshTokensAndPermissions,
    revokeToken,
    openRevokeDialog,
    closeRevokeDialog,
  };
}

function useTokenIssuance(loadTokens: () => Promise<GatewayToken[]>) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [form, setForm] = useState(emptyForm);
  const [createdToken, setCreatedToken] = useState<CreatedGatewayToken | null>(null);
  const issuancePending = useRef(false);
  const { actionState, runAction, resetAction } = useAsyncAction();

  async function createToken(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (issuancePending.current) return;
    issuancePending.current = true;
    try {
      const token = await runAction({
        pending: "saving",
        successMessage: "Token created.",
        action: async () => {
          const created = gatewayCreatedTokenResponse(await apiPost("/api/tokens", tokenCreatePayload(form)));
          await loadTokens();
          return created;
        },
      });
      if (!token) return;
      setCreatedToken(token);
      setForm(emptyForm);
      setDrawerOpen(false);
    } finally {
      issuancePending.current = false;
    }
  }

  function setDrawer(open: boolean) {
    if (actionState.state === "saving") return;
    resetAction();
    setDrawerOpen(open);
  }

  return {
    actionState,
    closeDrawer: () => setDrawer(false),
    createToken,
    createdToken,
    dismissCreatedToken: () => setCreatedToken(null),
    drawerOpen,
    form,
    openDrawer: () => setDrawer(true),
    setForm,
  };
}
