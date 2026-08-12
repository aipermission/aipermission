export function HighlightedText({ text, query }) {
  const value = String(text || "");
  const needle = String(query || "").trim();
  if (!needle) return value;

  const lowerValue = value.toLowerCase();
  const lowerNeedle = needle.toLowerCase();
  const parts = [];
  let cursor = 0;
  let match = lowerValue.indexOf(lowerNeedle);
  while (match >= 0) {
    if (match > cursor) parts.push(value.slice(cursor, match));
    parts.push(
      <mark key={`${match}-${parts.length}`} className="bg-amber-300 px-0 text-stone-950">
        {value.slice(match, match + needle.length)}
      </mark>,
    );
    cursor = match + needle.length;
    match = lowerValue.indexOf(lowerNeedle, cursor);
  }
  if (cursor < value.length) parts.push(value.slice(cursor));
  return parts;
}
