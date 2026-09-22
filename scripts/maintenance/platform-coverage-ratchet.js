function approvedPlatformCoverageAddition(base, current, name) {
  const prefix = "backend.coverage.platform.";
  if (!name.startsWith(prefix)) return false;
  const sourceMarker = Object.keys(current)
    .filter(
      (key) =>
        key.startsWith(prefix) &&
        current[key] === 0 &&
        !key.includes(".constraint.") &&
        !key.includes(".evidence."),
    )
    .sort((left, right) => right.length - left.length)
    .find((key) => name === key || name.startsWith(`${key}.`));
  if (!sourceMarker || Object.hasOwn(base, sourceMarker)) return false;

  const related = Object.keys(current).filter(
    (key) => key === sourceMarker || key.startsWith(`${sourceMarker}.`),
  );
  const constraints = related.filter((key) =>
    key.startsWith(`${sourceMarker}.constraint.`),
  );
  if (constraints.length !== 1) return false;
  const constraint = constraints[0];
  const platform = constraint.slice(`${sourceMarker}.constraint.`.length);
  if (!new Set(["windows", "darwin"]).has(platform)) return false;
  const evidencePrefix = `${sourceMarker}.evidence.`;
  const evidence = related.filter((key) => key.startsWith(evidencePrefix));
  const allowed = new Set([sourceMarker, constraint, ...evidence]);
  return (
    current[constraint] === -100 &&
    evidence.length > 0 &&
    related.every((key) => allowed.has(key)) &&
    evidence.every(
      (key) =>
        current[key] === 0 &&
        current[
          `test.${platform}.runtime.${key.slice(evidencePrefix.length)}`
        ] === 0,
    ) &&
    allowed.has(name)
  );
}

function approvedPlatformCoverageRelocation(base, current, name) {
  const match = name.match(/^test\.(windows|darwin)\.runtime\.(.+):([^:]+)$/);
  if (!match) return false;
  const [, platform, previousPackage, testName] = match;
  const previousEvidenceSuffix = `.evidence.${previousPackage}:${testName}`;
  const previousEvidence = Object.keys(base).filter(
    (key) =>
      key.startsWith("backend.coverage.platform.") &&
      key.endsWith(previousEvidenceSuffix),
  );
  if (previousEvidence.length !== 1) return false;
  const previousSource = previousEvidence[0].slice(
    0,
    -previousEvidenceSuffix.length,
  );
  if (Object.hasOwn(current, previousSource)) return false;

  const runtimePrefix = `test.${platform}.runtime.`;
  const replacements = Object.keys(current).filter(
    (key) => key.startsWith(runtimePrefix) && key.endsWith(`:${testName}`),
  );
  if (replacements.length !== 1) return false;
  const replacementIdentity = replacements[0].slice(runtimePrefix.length);
  const replacementEvidenceSuffix = `.evidence.${replacementIdentity}`;
  const replacementEvidence = Object.keys(current).filter(
    (key) =>
      key.startsWith("backend.coverage.platform.") &&
      key.endsWith(replacementEvidenceSuffix),
  );
  return (
    replacementEvidence.length === 1 &&
    approvedPlatformCoverageAddition(base, current, replacementEvidence[0])
  );
}

module.exports = {
  approvedPlatformCoverageAddition,
  approvedPlatformCoverageRelocation,
};
