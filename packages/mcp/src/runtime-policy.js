export const minimumRuntimeVersions = Object.freeze(["22.23.4", "24.22.1", "26.12.1"]);
export const supportedNodeEngines = minimumRuntimeVersions
  .map((version, index) => `${index === minimumRuntimeVersions.length - 1 ? ">=" : "^"}${version}`)
  .join(" || ");

export function isSupportedNodeRuntime(version) {
  if (typeof version !== "string" || !/^\d+\.\d+\.\d+$/.test(version)) return false;
  const actual = version.split(".").map(Number);
  if (!actual.every(Number.isSafeInteger)) return false;
  const floors = minimumRuntimeVersions.map((value) => value.split(".").map(Number));
  if (actual[0] > floors.at(-1)[0]) return true;
  return floors.some(([major, minor, patch]) => actual[0] === major && (actual[1] > minor || (actual[1] === minor && actual[2] >= patch)));
}

export function assertSupportedNodeRuntime(version = process.versions.node) {
  if (!isSupportedNodeRuntime(version)) {
    throw new Error(
      `Unsupported Node.js ${version}. AIPermission MCP requires ${supportedNodeEngines}. Install a patched supported Node.js release and restart the AI client.`,
    );
  }
}
