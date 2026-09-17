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
  return String(value || "")
    .trim()
    .replace(/^v/i, "");
}

export function compareVersions(a, b) {
  const left = versionParts(a);
  const right = versionParts(b);
  for (let index = 0; index < 3; index += 1) {
    const comparison = compareNumericIdentifier(left.numbers[index], right.numbers[index]);
    if (comparison !== 0) return comparison;
  }
  if (left.prerelease.length === 0 && right.prerelease.length === 0) return 0;
  if (left.prerelease.length === 0) return 1;
  if (right.prerelease.length === 0) return -1;
  for (let index = 0; index < Math.max(left.prerelease.length, right.prerelease.length); index += 1) {
    if (index >= left.prerelease.length) return -1;
    if (index >= right.prerelease.length) return 1;
    const comparison = comparePrereleaseIdentifier(left.prerelease[index], right.prerelease[index]);
    if (comparison !== 0) return comparison;
  }
  return 0;
}

function versionParts(value) {
  const withoutBuild = normalizeVersion(value).split("+", 1)[0];
  const prereleaseIndex = withoutBuild.indexOf("-");
  const core = prereleaseIndex >= 0 ? withoutBuild.slice(0, prereleaseIndex) : withoutBuild;
  const prerelease = prereleaseIndex >= 0 ? withoutBuild.slice(prereleaseIndex + 1).split(".") : [];
  const numbers = core.split(".").slice(0, 3);
  while (numbers.length < 3) numbers.push("0");
  return { numbers: numbers.map((part) => (/^\d+$/.test(part) ? part : "0")), prerelease };
}

function comparePrereleaseIdentifier(left, right) {
  const leftNumeric = /^\d+$/.test(left);
  const rightNumeric = /^\d+$/.test(right);
  if (leftNumeric && rightNumeric) return compareNumericIdentifier(left, right);
  if (leftNumeric) return -1;
  if (rightNumeric) return 1;
  return left > right ? 1 : left < right ? -1 : 0;
}

function compareNumericIdentifier(left, right) {
  const normalizedLeft = left.replace(/^0+(?=\d)/, "");
  const normalizedRight = right.replace(/^0+(?=\d)/, "");
  if (normalizedLeft.length !== normalizedRight.length) return normalizedLeft.length > normalizedRight.length ? 1 : -1;
  return normalizedLeft > normalizedRight ? 1 : normalizedLeft < normalizedRight ? -1 : 0;
}
