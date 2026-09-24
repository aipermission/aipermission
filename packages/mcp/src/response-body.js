export const MAX_GATEWAY_RESPONSE_BYTES = 8 * 1024 * 1024;

export async function readGatewayResponseText(response) {
  if (!response.body) return "";
  const reader = response.body.getReader();
  const chunks = [];
  let received = 0;
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      received += value.byteLength;
      if (received > MAX_GATEWAY_RESPONSE_BYTES) {
        await reader.cancel();
        throw new Error(`Gateway response exceeds ${MAX_GATEWAY_RESPONSE_BYTES} bytes.`);
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }
  return new TextDecoder().decode(Buffer.concat(chunks, received));
}
