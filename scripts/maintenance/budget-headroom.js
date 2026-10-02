function budgetHeadroom(label, used, limit) {
  if (!Number.isInteger(used) || used < 0 || !Number.isInteger(limit) || limit <= 0) {
    throw new TypeError("Budget measurements must use non-negative counts and positive limits");
  }
  if (used > limit || used * 100 < limit * 85) return null;
  const threshold = used * 100 >= limit * 90 ? 90 : 85;
  return `${label}: ${used}/${limit} lines (${threshold}% headroom warning); hard limit unchanged`;
}

module.exports = { budgetHeadroom };
