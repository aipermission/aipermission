export function isValidDatabasePassword(value: string | null | undefined): boolean {
  const text = String(value || "");
  return text.length >= 14 && /[A-Z]/.test(text) && /[a-z]/.test(text) && /[0-9]/.test(text);
}
