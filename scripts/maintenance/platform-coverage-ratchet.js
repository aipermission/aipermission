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

module.exports = { approvedPlatformCoverageAddition };
