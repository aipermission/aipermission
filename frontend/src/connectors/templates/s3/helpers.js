export function fileToBase64(file) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result || "");
      resolve(result.includes(",") ? result.split(",").pop() : result);
    };
    reader.onerror = () => reject(reader.error || new Error("Failed to read file."));
    reader.readAsDataURL(file);
  });
}

export function base64Blob(value, contentType) {
  const binary = atob(value || "");
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return new Blob([bytes], { type: contentType || "application/octet-stream" });
}

export function filenameFromKey(key) {
  const parts = String(key || "s3-object")
    .split("/")
    .filter(Boolean);
  return parts[parts.length - 1] || "s3-object";
}

export function safeDownloadName(value) {
  return String(value || "s3-object").replaceAll(":", "-");
}

export function normalizeObjectKey(value) {
  return String(value || "")
    .trim()
    .replace(/^\/+/, "");
}

export function joinObjectKey(prefix, name) {
  const cleanPrefix = normalizeObjectKey(prefix);
  const cleanName = normalizeObjectKey(name);
  if (!cleanPrefix) return cleanName;
  return `${cleanPrefix.replace(/\/+$/, "")}/${cleanName}`;
}

export function parentPrefix(value) {
  const clean = normalizeObjectKey(value).replace(/\/+$/, "");
  const index = clean.lastIndexOf("/");
  if (index <= 0) return "";
  return `${clean.slice(0, index)}/`;
}

export function shortDate(value) {
  if (!value) return "unknown";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}
