export const responsiveViewportMatrix = Object.freeze([
  Object.freeze({ width: 320, height: 900 }),
  Object.freeze({ width: 360, height: 900 }),
  Object.freeze({ width: 390, height: 900 }),
  Object.freeze({ width: 1024, height: 900 }),
  Object.freeze({ width: 1280, height: 900 }),
]);

const responsiveVaultTitles = responsiveViewportMatrix.map(
  ({ width, height }) => `@high-risk keeps Vault permission completion reachable at ${width}x${height}`,
);
const responsiveWorkspaceTitles = responsiveViewportMatrix.map(
  ({ width, height }) => `@high-risk keeps navigation, Console drawers, and permission dialogs usable at ${width}x${height}`,
);

export const requiredHighRiskTitles = Object.freeze([
  "@high-risk unlocks the local UI session and renders the dashboard",
  "@high-risk imports a database from the unlock screen",
  "@high-risk updates token connector permission from the Tokens page",
  "@high-risk updates project Vault permissions from the Tokens page",
  "@high-risk reviews and runs a Prompt connector action in the selected target context",
  "@high-risk keeps structured sessions isolated while switching connector profiles",
  "@high-risk reconnects a live console after the remote session exits",
  "@high-risk cancels an active transfer from the transfer center",
  ...responsiveVaultTitles,
  ...responsiveWorkspaceTitles,
]);

export const requiredAccessibilityTitles = Object.freeze([
  "@accessibility keeps modal focus contained and returns it to the opener",
  "@accessibility keeps primary unlocked pages accessible",
  "@accessibility keeps unlock tabs usable at 320px",
  "@accessibility keeps setup tabs usable at 320px",
  "@accessibility keeps unlock tabs usable at 360px",
  "@accessibility keeps setup tabs usable at 360px",
]);

export const requiredSmokeTitles = Object.freeze([
  "renders security settings and updates MCP metadata exposure",
  "renders settings retention controls",
  "moves an edited connector to another project",
]);

export const requiredRealBackendTitles = Object.freeze(["runs approval, stale rejection, lock, and restart against the real backend"]);
