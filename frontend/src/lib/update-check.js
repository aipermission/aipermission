export async function checkForUpdates(currentVersion) {
  const release = await fetchLatestRelease();
  const latestVersion = normalizeVersion(release.tag_name || release.name || "");
  const localVersion = normalizeVersion(currentVersion);
  return {
    latestVersion,
    localVersion,
    releaseUrl: release.html_url || "https://github.com/aipermission/aipermission/releases",
    updateAvailable: compareVersions(latestVersion, localVersion) > 0,
  };
}

async function fetchLatestRelease() {
  const latestResponse = await fetch("https://api.github.com/repos/aipermission/aipermission/releases/latest", {
    headers: { Accept: "application/vnd.github+json" },
  });
  if (latestResponse.ok) {
    return latestResponse.json();
  }
  if (latestResponse.status !== 404) {
    throw new Error(`GitHub release check failed with ${latestResponse.status}`);
  }

  const releasesResponse = await fetch("https://api.github.com/repos/aipermission/aipermission/releases?per_page=1", {
    headers: { Accept: "application/vnd.github+json" },
  });
  if (!releasesResponse.ok) {
    throw new Error(`GitHub release check failed with ${releasesResponse.status}`);
  }
  const releases = await releasesResponse.json();
  if (!Array.isArray(releases) || releases.length === 0) {
    throw new Error("No GitHub releases found.");
  }
  return releases[0];
}

function normalizeVersion(value) {
  return String(value || "").trim().replace(/^v/i, "");
}

function compareVersions(a, b) {
  const left = versionParts(a);
  const right = versionParts(b);
  for (let index = 0; index < 3; index += 1) {
    if (left[index] > right[index]) return 1;
    if (left[index] < right[index]) return -1;
  }
  if (left[3] === right[3]) return 0;
  if (left[3] === "") return 1;
  if (right[3] === "") return -1;
  return left[3] > right[3] ? 1 : left[3] < right[3] ? -1 : 0;
}

function versionParts(value) {
  const [numbers, prerelease = ""] = normalizeVersion(value).split("-", 2);
  const parts = numbers.split(".").map((part) => Number.parseInt(part, 10));
  return [parts[0] || 0, parts[1] || 0, parts[2] || 0, prerelease];
}
