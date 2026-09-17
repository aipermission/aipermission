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
  const idempotencyKey =
    error instanceof Error && typeof error.idempotencyKey === "string" && error.idempotencyKey.length <= 128 ? error.idempotencyKey : "";
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
            ...(idempotencyKey ? { idempotency_key: idempotencyKey } : {}),
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

export async function jsonToolResult(callback, project, mutationContext = null) {
  if (typeof project !== "function") {
    return errorResult(new Error("MCP tool result projector is required."));
  }
  let value;
  try {
    value = await callback();
  } catch (error) {
    return errorResult(error);
  }
  try {
    return textResult(project(value));
  } catch (error) {
    return errorResult(mutationContext ? projectionOutcomeUnknown(error, mutationContext) : error);
  }
}

function projectionOutcomeUnknown(cause, context) {
  const error = new Error(
    "The gateway accepted the mutation, but its response failed MCP contract validation. The operation may have completed.",
    { cause },
  );
  error.resultStatus = "outcome_unknown";
  error.code = "gateway_response_contract_outcome_unknown";
  if (typeof context.idempotencyKey === "string") error.idempotencyKey = context.idempotencyKey;
  if (Number.isSafeInteger(context.requestID) && context.requestID > 0) error.requestID = context.requestID;
  error.assistantHint = context.idempotencyKey
    ? "Reconcile the original request using the same idempotency key and unchanged input. Never retry with a new key blindly."
    : "Inspect the original request status before repeating the operation.";
  return error;
}
