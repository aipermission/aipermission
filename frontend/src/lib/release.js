import release from "./release.generated.json" with { type: "json" };

export const appVersion = release.version;
export const changelogEntries = release.entries;
