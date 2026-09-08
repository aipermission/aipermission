export const vaultSecretTypes = [
  ["generic_secret", "Generic secret"],
  ["api_key", "API key"],
  ["access_token", "Access token"],
  ["password", "Password"],
  ["client_secret", "Client secret"],
  ["webhook_hmac", "Webhook / HMAC secret"],
  ["connection", "Connection string"],
];

export const vaultGeneratorKinds = [
  ["random_token", "Random token (32 bytes)"],
  ["hex_secret", "Hex secret (32 bytes)"],
  ["password", "Password (32 characters)"],
  ["long_hmac_secret", "Long HMAC secret (64 bytes)"],
  ["uuid_v4", "UUID v4 (identifier)"],
];
