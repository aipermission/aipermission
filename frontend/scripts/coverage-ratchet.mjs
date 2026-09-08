export const coverageFloors = { statements: 75, branches: 60, functions: 70, lines: 75 };
export const coverageDebtStep = 1;

export function ratchetedMetrics(previous, floors = coverageFloors) {
  if (!previous) return { ...floors };
  return Object.fromEntries(
    Object.entries(floors).map(([metric, floor]) => {
      const accepted = previous[metric];
      if (!Number.isFinite(accepted)) throw new Error(`Base coverage baseline is missing ${metric}`);
      return [metric, accepted >= floor ? accepted : Math.min(floor, roundCoverage(accepted + coverageDebtStep))];
    }),
  );
}

export function requiredChangedMetrics({ baseBaselineAvailable, previous, added, accepted }, floors = coverageFloors) {
  if (baseBaselineAvailable) return ratchetedMetrics(previous, floors);
  if (added) return ratchetedMetrics(null, floors);
  return { ...accepted };
}

export function validateCoverageBaseline(baseline, owners, floors = coverageFloors) {
  if (baseline?.version !== 2 || !baseline.files || typeof baseline.files !== "object") {
    throw new Error("Changed coverage baseline must use version 2");
  }
  for (const [metric, floor] of Object.entries(floors)) {
    if (baseline.floors?.[metric] !== floor) throw new Error(`Changed coverage baseline floor mismatch for ${metric}`);
  }
  if (owners) {
    const expected = [...owners].sort();
    const actual = Object.keys(baseline.files).sort();
    if (JSON.stringify(expected) !== JSON.stringify(actual)) {
      const missing = expected.filter((file) => !Object.hasOwn(baseline.files, file));
      const unknown = actual.filter((file) => !owners.includes(file));
      throw new Error(
        `Changed coverage baseline owner mismatch (missing: ${missing.join(", ") || "none"}; unknown: ${unknown.join(", ") || "none"})`,
      );
    }
  }
  for (const [file, metrics] of Object.entries(baseline.files)) {
    for (const metric of Object.keys(floors)) {
      if (!Number.isFinite(metrics?.[metric]) || metrics[metric] < 0 || metrics[metric] > 100) {
        throw new Error(`Invalid ${metric} coverage baseline for ${file}`);
      }
    }
  }
}

export function validateCoverageBaselineForRun(baseline, owners, updateBaseline, floors = coverageFloors) {
  validateCoverageBaseline(baseline, updateBaseline ? undefined : owners, floors);
}

export function mergeCoverageMetrics(previous, actual, floors = coverageFloors) {
  return Object.fromEntries(
    Object.keys(floors).map((metric) => {
      const prior = previous?.[metric];
      const measured = actual?.[metric];
      if (!Number.isFinite(measured)) throw new Error(`Measured coverage is missing ${metric}`);
      return [metric, Number.isFinite(prior) ? Math.max(prior, measured) : measured];
    }),
  );
}

export function mergeChangedCoverageBaseline(owners, changedOwners, currentFiles, measuredFiles, requiredFiles = {}) {
  const changed = new Set(changedOwners);
  return Object.fromEntries(
    owners.map((file) => [
      file,
      changed.has(file)
        ? mergeCoverageMetrics(mergeCoverageMetrics(currentFiles?.[file], measuredFiles[file]), requiredFiles[file] || measuredFiles[file])
        : currentFiles?.[file] || measuredFiles[file],
    ]),
  );
}

function roundCoverage(value) {
  return Math.round(value * 100) / 100;
}
