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
  return {
    isError: true,
    content: [
      {
        type: "text",
        text: JSON.stringify({ status: "error", ...(code ? { code } : {}), error: message }, null, 2),
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
