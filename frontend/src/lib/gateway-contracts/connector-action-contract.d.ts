export function assertConnectorActionResponse(
  _value: unknown,
  _expected?: { targetRef: string; actionName: string },
): Record<string, unknown>;

export function isConnectorActionStatus(_value: unknown): boolean;
export function isConnectorRetryPolicy(_value: unknown): boolean;
