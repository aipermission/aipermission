const maxBufferedDownloadBytes = 64 * 1024 * 1024;
const unsupportedDownloadMessage = "This browser cannot safely buffer this download. Use a browser with a streaming Save dialog.";
const oversizedDownloadMessage = "This download exceeds the 64 MiB browser buffer limit. Use a browser with a streaming Save dialog.";

export async function readBufferedDownload(response) {
  const lengthHeader = response.headers?.get("Content-Length");
  const declaredLength = lengthHeader === null || lengthHeader === undefined ? NaN : Number(lengthHeader);
  if (Number.isFinite(declaredLength) && declaredLength > maxBufferedDownloadBytes) {
    await cancelResponseBody(response);
    throw new Error(oversizedDownloadMessage);
  }
  if (response.body && typeof response.body.getReader === "function") {
    const reader = response.body.getReader();
    const chunks = [];
    let size = 0;
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        size += value.byteLength;
        if (size > maxBufferedDownloadBytes) {
          try {
            await reader.cancel();
          } catch {
            // Report the size limit even when the stream cannot be canceled.
          }
          throw new Error(oversizedDownloadMessage);
        }
        chunks.push(value);
      }
    } finally {
      reader.releaseLock();
    }
    return new Blob(chunks, { type: response.headers?.get("Content-Type") || "" });
  }
  throw new Error(unsupportedDownloadMessage);
}

async function cancelResponseBody(response) {
  try {
    await response.body?.cancel?.();
  } catch {
    // The size limit is the actionable error even if cancellation fails.
  }
}
