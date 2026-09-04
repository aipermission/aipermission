export function textResult(value) {
  const text = typeof value === "string" ? value : JSON.stringify(value, null, 2);
  return {
    content: [
      {
        type: "text",
        text,
      },
    ],
  };
}

export function errorResult(error) {
  const message = error instanceof Error ? error.message : String(error || "Unknown aipermission MCP error");
  const code = error instanceof Error && typeof error.code === "string" ? error.code : "";
  const status = error instanceof Error && typeof error.resultStatus === "string" ? error.resultStatus : "error";
  const requestID = error instanceof Error && Number.isSafeInteger(error.requestID) && error.requestID > 0 ? error.requestID : null;
  const assistantHint = error instanceof Error && typeof error.assistantHint === "string" ? error.assistantHint : "";
  const retryAfterSeconds =
    error instanceof Error && Number.isSafeInteger(error.retryAfterSeconds) && error.retryAfterSeconds >= 0
      ? error.retryAfterSeconds
      : null;
  return {
    isError: true,
    content: [
      {
        type: "text",
        text: JSON.stringify(
          {
            status,
            ...(code ? { code } : {}),
            ...(requestID ? { request_id: requestID } : {}),
            ...(assistantHint ? { assistant_hint: assistantHint } : {}),
            ...(retryAfterSeconds !== null ? { retry_after_seconds: retryAfterSeconds } : {}),
            error: message,
          },
          null,
          2,
        ),
      },
    ],
  };
}

export async function jsonToolResult(callback) {
  try {
    return textResult(await callback());
  } catch (error) {
    return errorResult(error);
  }
}
