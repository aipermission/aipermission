function abortError(): DOMException {
  return new DOMException("The operation was aborted.", "AbortError");
}

async function blobArrayBuffer(blob: Blob): Promise<ArrayBuffer> {
  if (typeof blob?.arrayBuffer === "function") return blob.arrayBuffer();
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error || new Error("Could not read the selected file."));
    reader.onload = () => {
      if (reader.result instanceof ArrayBuffer) resolve(reader.result);
      else reject(new Error("Could not read the selected file."));
    };
    reader.readAsArrayBuffer(blob);
  });
}

export async function fileSHA256(file: Blob, signal?: AbortSignal): Promise<string> {
  if (signal?.aborted) throw abortError();
  if (!globalThis.crypto?.subtle) throw new Error("Secure file hashing is unavailable; the file was not uploaded.");
  const bytes = await blobArrayBuffer(file);
  if (signal?.aborted) throw abortError();
  const digest = await globalThis.crypto.subtle.digest("SHA-256", bytes);
  if (signal?.aborted) throw abortError();
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}
