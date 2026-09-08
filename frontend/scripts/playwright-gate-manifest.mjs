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
]);

export const requiredRealBackendTitles = Object.freeze(["runs approval, stale rejection, lock, and restart against the real backend"]);
