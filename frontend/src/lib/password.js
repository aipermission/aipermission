export function isValidDatabasePassword(value) {
  return String(value || "").length >= 14 && /[A-Z]/.test(value) && /[a-z]/.test(value) && /[0-9]/.test(value);
}
