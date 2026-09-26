export type ProjectSummary = { id: number; name: string; slug: string; target_count: number };

export function projectListResponse(value: unknown): ProjectSummary[] {
  if (!value || typeof value !== "object" || Array.isArray(value) || !("items" in value)) throw invalidResponse();
  const items: unknown = value.items ?? [];
  if (!Array.isArray(items) || !items.every(validProject)) throw invalidResponse();
  return items.map(({ id, name, slug, target_count }) => ({ id, name, slug, target_count }));
}

function validProject(value: unknown): value is ProjectSummary {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return (
    "id" in value &&
    typeof value.id === "number" &&
    Number.isSafeInteger(value.id) &&
    value.id > 0 &&
    "name" in value &&
    typeof value.name === "string" &&
    "slug" in value &&
    typeof value.slug === "string" &&
    value.slug.length > 0 &&
    "target_count" in value &&
    typeof value.target_count === "number" &&
    Number.isSafeInteger(value.target_count) &&
    value.target_count >= 0
  );
}

function invalidResponse() {
  return new Error("Invalid project list response.");
}
