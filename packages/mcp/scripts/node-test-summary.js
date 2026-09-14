export function nodeTestSummaryCount(output, label) {
  const escapedLabel = label.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = new RegExp(`^(?:#|ℹ)\\s+${escapedLabel}\\s+(\\d+)\\s*$`, "m").exec(output);
  if (!match) throw new Error(`Node test output did not report ${label}`);
  return Number(match[1]);
}
